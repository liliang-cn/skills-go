package client

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/liliang-cn/skills-go/mcp"
	"github.com/liliang-cn/skills-go/renderer"
	"github.com/liliang-cn/skills-go/skill"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

// Client is the LLM skills client
type Client struct {
	client              *openai.Client
	model               string
	skills              *skill.Registry
	renderer            *renderer.Renderer
	executor            *skill.Executor
	config              *Config
	skillTrustPolicy    skill.TrustPolicy
	replaySessionSkills bool
	sessions            map[string]*skillSession
	sessionsMu          sync.RWMutex
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

// WithSkillTrustPolicy sets the trust policy used during skill discovery.
func WithSkillTrustPolicy(policy skill.TrustPolicy) ClientOption {
	return func(c *Client) {
		c.skillTrustPolicy = policy
	}
}

// WithProjectSkillsTrusted controls whether project-level skills are trusted by default.
func WithProjectSkillsTrusted(trusted bool) ClientOption {
	return func(c *Client) {
		if trusted {
			c.skillTrustPolicy = nil
			return
		}
		c.skillTrustPolicy = func(scope skill.SkillScope, skillPath string) (bool, string) {
			if scope == skill.SkillScopeProject {
				return false, "project-level skills are not trusted"
			}
			return true, ""
		}
	}
}

// WithSessionSkillReplay controls whether activated skills are replayed for subsequent turns in the same session.
func WithSessionSkillReplay(enabled bool) ClientOption {
	return func(c *Client) {
		c.replaySessionSkills = enabled
	}
}

// NewClient creates a new LLM skills client
func NewClient(cfg *Config, opts ...ClientOption) *Client {
	if cfg == nil {
		cfg = &Config{}
	}

	c := &Client{
		model:               cfg.Model,
		config:              cfg,
		renderer:            renderer.NewRenderer(),
		executor:            skill.NewExecutor(),
		replaySessionSkills: true,
		sessions:            make(map[string]*skillSession),
	}

	// Apply options first (to potentially set client or paths)
	for _, opt := range opts {
		opt(c)
	}

	// Initialize Loader/Registry
	loader := skill.NewLoader(
		skill.WithPaths(c.config.SkillPaths...),
		skill.WithTrustPolicy(c.skillTrustPolicy),
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

	return c.chatWithSkillTooling(ctx, userMessage, cfg)
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

	s, err := c.skills.GetWithLevel(ctx, skillName, skill.LoadLevelContent)
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
	if len(skills) == 0 {
		return ""
	}

	entries := buildSkillCatalogEntries(skills)
	var sb strings.Builder
	sb.WriteString("The following Agent Skills provide specialized instructions for specific tasks.\n")
	sb.WriteString("When a task matches a skill's description, call the activate_skill tool with the skill's name before proceeding.\n")
	sb.WriteString("When an activated skill references relative paths, resolve them against the skill directory returned by the tool.\n\n")
	sb.WriteString("<available_skills>\n")

	for _, entry := range entries {
		sb.WriteString("  <skill>\n")
		sb.WriteString("    <name>" + entry.Name + "</name>\n")
		sb.WriteString("    <description>" + escapeXML(entry.Description) + "</description>\n")
		sb.WriteString("    <location>" + escapeXML(entry.Location) + "</location>\n")
		sb.WriteString("  </skill>\n")
	}
	sb.WriteString("</available_skills>")

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

// GetSkillWithLevel returns a skill by name and upgrades it on demand.
func (c *Client) GetSkillWithLevel(ctx context.Context, name string, level skill.LoadLevel) (*skill.Skill, error) {
	return c.skills.GetWithLevel(ctx, name, level)
}

// SkillDiagnostics returns non-fatal skill discovery diagnostics such as collisions or trust skips.
func (c *Client) SkillDiagnostics() []skill.Diagnostic {
	return c.skills.Diagnostics()
}

// ReloadSkills reloads all skills
func (c *Client) ReloadSkills(ctx context.Context) error {
	c.skills.Clear()
	return c.skills.Load(ctx)
}

// ExecuteScript runs a script from a skill
func (c *Client) ExecuteScript(ctx context.Context, skillName, scriptName string, args ...string) (*skill.Result, error) {
	s, err := c.skills.GetWithLevel(ctx, skillName, skill.LoadLevelFull)
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
	s, err := c.skills.GetWithLevel(context.Background(), skillName, skill.LoadLevelFull)
	if err != nil {
		return nil, err
	}
	return skill.ListScripts(s), nil
}

// SetScriptTimeout sets the timeout for script execution
func (c *Client) SetScriptTimeout(timeout int) {
	c.executor = skill.NewExecutor(skill.WithTimeout(duration(timeout)))
}

// MCP Support

// mcpManager holds the MCP server manager (lazy initialized)
type mcpManager struct {
	manager *mcp.Manager
}

var (
	mcpManagers      = make(map[string]*mcp.Manager)
	mcpManagersMutex sync.RWMutex
)

// ConvertMCPServer converts an MCP server to a Skill
func (c *Client) ConvertMCPServer(ctx context.Context, cfg *mcp.ServerConfig, outputDir string) (*skill.Skill, error) {
	converter := mcp.NewConverter()
	return converter.Convert(ctx, cfg, outputDir)
}

// ConvertMCPServerCommand converts a command-based MCP server to a Skill
func (c *Client) ConvertMCPServerCommand(ctx context.Context, name string, command string, args ...string) (*skill.Skill, error) {
	cfg := mcp.NewCommand(name, command, args...)
	return c.ConvertMCPServer(ctx, cfg, c.defaultSkillOutputDir())
}

// ConvertMCPServerHTTP converts an HTTP-based MCP server to a Skill
func (c *Client) ConvertMCPServerHTTP(ctx context.Context, name string, url string) (*skill.Skill, error) {
	cfg := mcp.NewHTTP(name, url)
	return c.ConvertMCPServer(ctx, cfg, c.defaultSkillOutputDir())
}

// DiscoverMCPServer discovers capabilities of an MCP server without converting
func (c *Client) DiscoverMCPServer(ctx context.Context, cfg *mcp.ServerConfig) (*mcp.ServerCapabilities, error) {
	converter := mcp.NewConverter()
	return converter.Discover(ctx, cfg)
}

// ConvertMCPServerWithLLM converts an MCP server to a Skill using LLM for enhanced content
func (c *Client) ConvertMCPServerWithLLM(ctx context.Context, cfg *mcp.ServerConfig, outputDir string) (*skill.Skill, error) {
	if c.client == nil {
		return nil, fmt.Errorf("LLM conversion requires an OpenAI client (provide APIKey or inject client)")
	}
	converter := mcp.NewConverter(mcp.WithLLMClient(c.client))
	return converter.ConvertWithLLM(ctx, cfg, outputDir)
}

// ConvertMCPServerCommandWithLLM converts a command-based MCP server to a Skill using LLM
func (c *Client) ConvertMCPServerCommandWithLLM(ctx context.Context, name string, command string, args ...string) (*skill.Skill, error) {
	cfg := mcp.NewCommand(name, command, args...)
	return c.ConvertMCPServerWithLLM(ctx, cfg, c.defaultSkillOutputDir())
}

// ConvertMCPServerHTTPWithLLM converts an HTTP-based MCP server to a Skill using LLM
func (c *Client) ConvertMCPServerHTTPWithLLM(ctx context.Context, name string, url string) (*skill.Skill, error) {
	cfg := mcp.NewHTTP(name, url)
	return c.ConvertMCPServerWithLLM(ctx, cfg, c.defaultSkillOutputDir())
}

func (c *Client) defaultSkillOutputDir() string {
	if len(c.config.SkillPaths) > 0 {
		return c.config.SkillPaths[0]
	}

	return skill.DefaultPaths()[0]
}

// MCPServerManager returns or creates an MCP manager for the client
func (c *Client) MCPServerManager() *mcp.Manager {
	mcpManagersMutex.Lock()
	defer mcpManagersMutex.Unlock()

	key := fmt.Sprintf("%p", c)
	if mgr, exists := mcpManagers[key]; exists {
		return mgr
	}

	mgr := mcp.NewManager()
	mcpManagers[key] = mgr
	return mgr
}

// ConnectMCPServer connects to an MCP server for runtime use
func (c *Client) ConnectMCPServer(ctx context.Context, cfg *mcp.ServerConfig) (*mcp.MCPServer, error) {
	return c.MCPServerManager().Connect(ctx, cfg)
}

// DisconnectMCPServer disconnects from an MCP server
func (c *Client) DisconnectMCPServer(ctx context.Context, name string) error {
	return c.MCPServerManager().Disconnect(ctx, name)
}

// DisconnectAllMCPServers disconnects all MCP servers
func (c *Client) DisconnectAllMCPServers(ctx context.Context) error {
	return c.MCPServerManager().DisconnectAll(ctx)
}

// GetMCPServer returns a connected MCP server by name
func (c *Client) GetMCPServer(name string) (*mcp.MCPServer, bool) {
	return c.MCPServerManager().GetServer(name)
}

// ListMCPServers returns all connected MCP server names
func (c *Client) ListMCPServers() []string {
	return c.MCPServerManager().ListServers()
}

// CallMCPTool calls a tool on a connected MCP server
func (c *Client) CallMCPTool(ctx context.Context, serverName, toolName string, args map[string]any) (string, error) {
	srv, ok := c.GetMCPServer(serverName)
	if !ok {
		return "", fmt.Errorf("MCP server %s not connected", serverName)
	}

	result, err := srv.CallTool(ctx, toolName, args)
	if err != nil {
		return "", err
	}

	if result.IsError {
		return "", fmt.Errorf("tool error: %s", formatContent(result.Content))
	}

	return formatContent(result.Content), nil
}

// ReadMCPResource reads a resource from a connected MCP server
func (c *Client) ReadMCPResource(ctx context.Context, serverName, uri string) (string, error) {
	srv, ok := c.GetMCPServer(serverName)
	if !ok {
		return "", fmt.Errorf("MCP server %s not connected", serverName)
	}

	result, err := srv.ReadResource(ctx, uri)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for _, c := range result.Contents {
		sb.WriteString(c.Text)
	}
	return sb.String(), nil
}

// GetMCPPrompt gets a prompt from a connected MCP server
func (c *Client) GetMCPPrompt(ctx context.Context, serverName, promptName string, args map[string]string) (string, map[string]any, error) {
	srv, ok := c.GetMCPServer(serverName)
	if !ok {
		return "", nil, fmt.Errorf("MCP server %s not connected", serverName)
	}

	result, err := srv.GetPrompt(ctx, promptName, args)
	if err != nil {
		return "", nil, err
	}

	// Build messages from result
	var messages strings.Builder
	var metadata = make(map[string]any)

	for _, msg := range result.Messages {
		if tc, ok := msg.Content.(*mcpsdk.TextContent); ok {
			messages.WriteString(tc.Text)
			messages.WriteString("\n")
		}
	}

	metadata["name"] = promptName

	return messages.String(), metadata, nil
}

// formatContent formats MCP content to a string
func formatContent(content []mcpsdk.Content) string {
	var sb strings.Builder
	for _, c := range content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

func duration(seconds int) time.Duration {
	return time.Duration(seconds) * time.Second
}
