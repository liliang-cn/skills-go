package client

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/liliang-cn/skills-go/renderer"
	"github.com/liliang-cn/skills-go/skill"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

// Client is the LLM skills client
type Client struct {
	client   *openai.Client
	model    string
	skills   *skill.Registry
	renderer *renderer.Renderer
	executor *skill.Executor
	config   *Config
}

// Config configures the client
type Config struct {
	APIKey     string
	BaseURL    string
	Model      string
	SkillPaths []string
	Timeout    int // seconds
}

// ClientOption configures the client
type ClientOption func(*Client)

// WithModel sets the default model
func WithModel(model string) ClientOption {
	return func(c *Client) {
		c.model = model
	}
}

// WithSkillPaths sets the skill search paths
func WithSkillPaths(paths ...string) ClientOption {
	return func(c *Client) {
		c.config.SkillPaths = append(c.config.SkillPaths, paths...)
	}
}

// WithOpenAIClient sets the OpenAI client instance
func WithOpenAIClient(client *openai.Client) ClientOption {
	return func(c *Client) {
		c.client = client
	}
}

// NewClient creates a new LLM skills client
func NewClient(cfg *Config, opts ...ClientOption) *Client {
	if cfg == nil {
		cfg = &Config{}
	}

	c := &Client{
		model:    cfg.Model,
		config:   cfg,
		renderer: renderer.NewRenderer(),
		executor: skill.NewExecutor(),
	}

	// Apply options first (to potentially set client or paths)
	for _, opt := range opts {
		opt(c)
	}

	// Initialize Loader/Registry
	loader := skill.NewLoader(
		skill.WithPaths(c.config.SkillPaths...),
	)
	c.skills = skill.NewRegistry(loader)

	// Initialize OpenAI client if not provided
	if c.client == nil && cfg.APIKey != "" {
		clientOpts := []option.RequestOption{
			option.WithAPIKey(cfg.APIKey),
		}
		if cfg.BaseURL != "" {
			clientOpts = append(clientOpts, option.WithBaseURL(cfg.BaseURL))
		}
		client := openai.NewClient(clientOpts...)
		c.client = &client
	}

	if c.model == "" {
		c.model = "gpt-4o"
	}

	return c
}

// Resolve identifies relevant skills for a user query
func (c *Client) Resolve(ctx context.Context, query string) ([]*skill.Skill, error) {
	return c.skills.Resolve(ctx, query)
}

// BuildSystemPrompt generates the system message addition for the provided skills
func (c *Client) BuildSystemPrompt(skills []*skill.Skill) string {
	return c.buildSystemMessage(skills)
}

// LoadSkills loads all skills from configured paths
func (c *Client) LoadSkills(ctx context.Context) error {
	return c.skills.Load(ctx)
}

// Chat sends a chat message with automatic skill matching
func (c *Client) Chat(ctx context.Context, userMessage string, opts ...ChatOption) (*ChatResponse, error) {
	if c.client == nil {
		return nil, fmt.Errorf("chat functionality requires an OpenAI client (provide APIKey or inject client)")
	}

	cfg := &chatConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Check if this is a direct skill invocation
	if strings.HasPrefix(userMessage, "/") {
		return c.invokeSkill(ctx, userMessage, cfg)
	}

	// Otherwise, find relevant skills
	relevantSkills, err := c.skills.Resolve(ctx, userMessage)
	if err != nil {
		return nil, err
	}

	// Build system message with skill descriptions
	systemMsg := c.buildSystemMessage(relevantSkills)

	// Build messages
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(systemMsg),
		openai.UserMessage(userMessage),
	}

	// Add conversation history if provided
	if cfg.History != nil {
		messages = append(cfg.History, messages...)
	}

	// Call OpenAI API
	resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: messages,
		Model:    shared.ChatModel(c.getModel()),
	})
	if err != nil {
		return nil, err
	}

	return &ChatResponse{
		Content:      resp.Choices[0].Message.Content,
		SkillsUsed:   skillNames(relevantSkills),
		FinishReason: string(resp.Choices[0].FinishReason),
		Usage: Usage{
			PromptTokens:     int(resp.Usage.PromptTokens),
			CompletionTokens: int(resp.Usage.CompletionTokens),
			TotalTokens:      int(resp.Usage.TotalTokens),
		},
	}, nil
}

// ChatOption configures a chat request
type ChatOption func(*chatConfig)

type chatConfig struct {
	SessionID   string
	Environment map[string]string
	Variables   map[string]string
	UserID      string
	IsUser      bool
	UserMessage string
	History     []openai.ChatCompletionMessageParamUnion
}

// WithSessionID sets the session ID
func WithSessionID(id string) ChatOption {
	return func(c *chatConfig) {
		c.SessionID = id
	}
}

// WithEnvironment sets environment variables
func WithEnvironment(env map[string]string) ChatOption {
	return func(c *chatConfig) {
		if c.Environment == nil {
			c.Environment = make(map[string]string)
		}
		for k, v := range env {
			c.Environment[k] = v
		}
	}
}

// WithVariables sets custom variables
func WithVariables(vars map[string]string) ChatOption {
	return func(c *chatConfig) {
		if c.Variables == nil {
			c.Variables = make(map[string]string)
		}
		for k, v := range vars {
			c.Variables[k] = v
		}
	}
}

// WithAsUser sets whether this is a user invocation
func WithAsUser(isUser bool) ChatOption {
	return func(c *chatConfig) {
		c.IsUser = isUser
	}
}

// WithHistory sets conversation history
func WithHistory(history []openai.ChatCompletionMessageParamUnion) ChatOption {
	return func(c *chatConfig) {
		c.History = history
	}
}

// invokeSkill directly invokes a specific skill
func (c *Client) invokeSkill(ctx context.Context, invocation string, cfg *chatConfig) (*ChatResponse, error) {
	skillName, args := skill.ParseArguments(invocation)
	if skillName == "" {
		return nil, skill.ErrInvalidInvocation
	}

	s, err := c.skills.Get(skillName)
	if err != nil {
		return nil, err
	}

	// Check if user can invoke
	if cfg.IsUser && !s.IsUserInvocable() {
		return nil, skill.ErrSkillNotUserInvocable
	}

	// Build invocation context
	invokeCtx := &renderer.InvocationContext{
		Arguments:   args,
		SessionID:   cfg.SessionID,
		Environment: cfg.Environment,
		Variables:   cfg.Variables,
		UserID:      cfg.UserID,
	}

	// Render skill content
	vars := renderer.BuildVars(invokeCtx)
	rendered, err := c.renderer.Render(ctx, s.Content, vars)
	if err != nil {
		return nil, err
	}

	// If skill has context: fork, run in subagent
	if s.ShouldFork() {
		return c.executeInSubagent(ctx, s, rendered, invokeCtx, cfg)
	}

	// Otherwise, use as system message
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(rendered),
	}

	// Add user message if provided
	if cfg.UserMessage != "" {
		messages = append(messages, openai.UserMessage(cfg.UserMessage))
	} else if len(args) > 0 && !strings.Contains(s.Content, "$ARGUMENTS") {
		// Append arguments if skill doesn't use them
		messages = append(messages, openai.UserMessage("ARGUMENTS: "+strings.Join(args, " ")))
	}

	resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: messages,
		Model:    shared.ChatModel(c.getModelForSkill(s)),
	})
	if err != nil {
		return nil, err
	}

	return &ChatResponse{
		Content:      resp.Choices[0].Message.Content,
		SkillsUsed:   []string{s.Name},
		FinishReason: string(resp.Choices[0].FinishReason),
		Usage: Usage{
			PromptTokens:     int(resp.Usage.PromptTokens),
			CompletionTokens: int(resp.Usage.CompletionTokens),
			TotalTokens:      int(resp.Usage.TotalTokens),
		},
	}, nil
}

// executeInSubagent executes a skill in a forked context
func (c *Client) executeInSubagent(ctx context.Context, s *skill.Skill, rendered string, invokeCtx *renderer.InvocationContext, cfg *chatConfig) (*ChatResponse, error) {
	// In a real implementation, this would create a separate context
	// For now, we'll use the system prompt approach
	agent := s.GetAgent()

	// Build agent-specific system prompt
	systemMsg := fmt.Sprintf("You are a %s agent. Execute the following task:\n\n%s", agent, rendered)

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(systemMsg),
	}

	// Add arguments as user message if provided
	if len(invokeCtx.Arguments) > 0 {
		messages = append(messages, openai.UserMessage(strings.Join(invokeCtx.Arguments, " ")))
	}

	resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: messages,
		Model:    shared.ChatModel(c.getModelForSkill(s)),
	})
	if err != nil {
		return nil, err
	}

	return &ChatResponse{
		Content:      resp.Choices[0].Message.Content,
		SkillsUsed:   []string{s.Name},
		FinishReason: string(resp.Choices[0].FinishReason),
		Subagent:     agent,
		Usage: Usage{
			PromptTokens:     int(resp.Usage.PromptTokens),
			CompletionTokens: int(resp.Usage.CompletionTokens),
			TotalTokens:      int(resp.Usage.TotalTokens),
		},
	}, nil
}

// buildSystemMessage builds a system message listing available skills
func (c *Client) buildSystemMessage(skills []*skill.Skill) string {
	var sb strings.Builder
	sb.WriteString("You are an AI assistant with the following skills available:\n\n")

	for _, s := range skills {
		sb.WriteString(fmt.Sprintf("- /%s: %s\n", s.Name, s.Meta.Description))
	}

	sb.WriteString("\nUse these skills when relevant to help the user.")

	return sb.String()
}

// ChatResponse is the response from a chat request
type ChatResponse struct {
	Content      string   `json:"content"`
	SkillsUsed   []string `json:"skills_used,omitempty"`
	FinishReason string   `json:"finish_reason,omitempty"`
	Subagent     string   `json:"subagent,omitempty"`
	Usage        Usage    `json:"usage,omitempty"`
}

// Usage represents token usage
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// skillNames extracts skill names from a slice of skills
func skillNames(skills []*skill.Skill) []string {
	names := make([]string, len(skills))
	for i, s := range skills {
		names[i] = s.Name
	}
	return names
}

// getModel returns the default model
func (c *Client) getModel() string {
	return c.model
}

// getModelForSkill returns the model for a specific skill
func (c *Client) getModelForSkill(s *skill.Skill) string {
	if s.GetModel() != "" {
		return s.GetModel()
	}
	return c.model
}

// ListSkills returns all registered skills
func (c *Client) ListSkills() []skill.Meta {
	return c.skills.List()
}

// ListSkillNames returns all skill names
func (c *Client) ListSkillNames() []string {
	return c.skills.Names()
}

// GetSkill returns a skill by name
func (c *Client) GetSkill(name string) (*skill.Skill, error) {
	return c.skills.Get(name)
}

// ReloadSkills reloads all skills
func (c *Client) ReloadSkills(ctx context.Context) error {
	c.skills.Clear()
	return c.skills.Load(ctx)
}

// ExecuteScript runs a script from a skill
func (c *Client) ExecuteScript(ctx context.Context, skillName, scriptName string, args ...string) (*skill.Result, error) {
	s, err := c.skills.Get(skillName)
	if err != nil {
		return nil, err
	}
	return c.executor.Execute(ctx, s, scriptName, args...)
}

// ExecuteScriptPath runs a script at a specific path
func (c *Client) ExecuteScriptPath(ctx context.Context, scriptPath string, args ...string) (*skill.Result, error) {
	return c.executor.ExecutePath(ctx, scriptPath, args...)
}

// ExecuteShell runs a shell command
func (c *Client) ExecuteShell(ctx context.Context, command string) (*skill.Result, error) {
	return c.executor.ExecuteShell(ctx, command)
}

// ExecuteInteractive runs a command interactively
func (c *Client) ExecuteInteractive(ctx context.Context, command string, args ...string) (*skill.Result, error) {
	return c.executor.ExecuteInteractive(ctx, command, args...)
}

// ListScripts returns all scripts in a skill
func (c *Client) ListScripts(skillName string) ([]skill.Script, error) {
	s, err := c.skills.Get(skillName)
	if err != nil {
		return nil, err
	}
	return skill.ListScripts(s), nil
}

// SetScriptTimeout sets the timeout for script execution
func (c *Client) SetScriptTimeout(timeout int) {
	c.executor = skill.NewExecutor(skill.WithTimeout(duration(timeout)))
}

func duration(seconds int) time.Duration {
	return time.Duration(seconds) * time.Second
}
