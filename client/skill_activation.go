package client

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/liliang-cn/skills-go/skill"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

const (
	activateSkillToolName = "activate_skill"
	maxSkillToolRounds    = 6
)

// SkillCatalogEntry is the tier-1 catalog entry disclosed to the model.
type SkillCatalogEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Location    string `json:"location"`
}

// ActivatedSkill is the structured result returned by activate_skill.
type ActivatedSkill struct {
	Name      string   `json:"name"`
	Location  string   `json:"location"`
	Directory string   `json:"directory"`
	Content   string   `json:"content"`
	Resources []string `json:"resources,omitempty"`
	Wrapped   string   `json:"wrapped"`
}

type activateSkillToolArgs struct {
	Name string `json:"name"`
}

// SkillCatalog returns the model-visible Agent Skills catalog.
func (c *Client) SkillCatalog() []SkillCatalogEntry {
	return buildSkillCatalogEntries(c.catalogSkills())
}

// ActivateSkill loads a skill's instructions and bundled resource listing.
func (c *Client) ActivateSkill(ctx context.Context, name string) (*ActivatedSkill, error) {
	s, err := c.skills.GetWithLevel(ctx, name, skill.LoadLevelFull)
	if err != nil {
		return nil, err
	}
	if s.Path == "" {
		return nil, fmt.Errorf("skill %q has no file-backed instructions", name)
	}

	activated := &ActivatedSkill{
		Name:      s.Name,
		Location:  skillLocation(s),
		Directory: skillDirectory(s),
		Content:   s.Content,
		Resources: skillResourcePaths(s),
	}
	activated.Wrapped = wrapActivatedSkill(activated)
	return activated, nil
}

func (c *Client) chatWithSkillTooling(ctx context.Context, userMessage string, cfg *chatConfig) (*ChatResponse, error) {
	skills := c.catalogSkills()
	protectedMessages := c.sessionProtectedMessages(cfg.SessionID, cfg.History)
	messages := buildChatMessages(cfg.History, c.buildSystemMessage(skills), protectedMessages, userMessage)
	tools := buildSkillTools(skills)
	activated := c.sessionActiveSet(cfg.SessionID)
	skillsUsed := make([]string, 0, len(skills))
	totalUsage := Usage{}
	finishReason := ""

	for i := 0; i < maxSkillToolRounds; i++ {
		params := openai.ChatCompletionNewParams{
			Messages: messages,
			Model:    shared.ChatModel(c.getModel()),
		}
		if len(tools) > 0 {
			params.Tools = tools
			params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{
				OfAuto: openai.String(string(openai.ChatCompletionToolChoiceOptionAutoAuto)),
			}
		}

		resp, err := c.client.Chat.Completions.New(ctx, params)
		if err != nil {
			return nil, err
		}
		if len(resp.Choices) == 0 {
			return nil, fmt.Errorf("chat completion returned no choices")
		}

		totalUsage.PromptTokens += int(resp.Usage.PromptTokens)
		totalUsage.CompletionTokens += int(resp.Usage.CompletionTokens)
		totalUsage.TotalTokens += int(resp.Usage.TotalTokens)

		choice := resp.Choices[0]
		finishReason = string(choice.FinishReason)
		messages = append(messages, choice.Message.ToParam())

		if len(choice.Message.ToolCalls) == 0 {
			return &ChatResponse{
				Content:      choice.Message.Content,
				SkillsUsed:   skillsUsed,
				FinishReason: finishReason,
				Usage:        totalUsage,
			}, nil
		}

		for _, toolCall := range choice.Message.ToolCalls {
			toolResult, activatedSkill := c.runSkillToolCall(ctx, toolCall, activated)
			if activatedSkill != nil {
				activated[activatedSkill.Name] = true
				skillsUsed = append(skillsUsed, activatedSkill.Name)
				c.rememberSessionSkill(cfg.SessionID, activatedSkill)
			}
			messages = append(messages, openai.ToolMessage(toolResult, toolCall.ID))
		}
	}

	return nil, fmt.Errorf("chat completion exceeded %d skill activation rounds", maxSkillToolRounds)
}

func (c *Client) runSkillToolCall(ctx context.Context, toolCall openai.ChatCompletionMessageToolCallUnion, activated map[string]bool) (string, *ActivatedSkill) {
	functionCall, ok := toolCall.AsAny().(openai.ChatCompletionMessageFunctionToolCall)
	if !ok {
		return wrapSkillActivationError("", fmt.Errorf("unsupported tool call type %q", toolCall.Type)), nil
	}
	if functionCall.Function.Name != activateSkillToolName {
		return wrapSkillActivationError("", fmt.Errorf("unexpected tool %q", functionCall.Function.Name)), nil
	}

	var args activateSkillToolArgs
	if err := json.Unmarshal([]byte(functionCall.Function.Arguments), &args); err != nil {
		return wrapSkillActivationError("", fmt.Errorf("invalid %s arguments: %w", activateSkillToolName, err)), nil
	}
	if args.Name == "" {
		return wrapSkillActivationError("", fmt.Errorf("%s requires a skill name", activateSkillToolName)), nil
	}
	if activated[args.Name] {
		return wrapDuplicateSkillActivation(args.Name), nil
	}

	activatedSkill, err := c.ActivateSkill(ctx, args.Name)
	if err != nil {
		return wrapSkillActivationError(args.Name, err), nil
	}

	return activatedSkill.Wrapped, activatedSkill
}

func buildChatMessages(history []openai.ChatCompletionMessageParamUnion, systemMsg string, protected []openai.ChatCompletionMessageParamUnion, userMessage string) []openai.ChatCompletionMessageParamUnion {
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(history)+len(protected)+2)
	if systemMsg != "" {
		messages = append(messages, openai.SystemMessage(systemMsg))
	}
	messages = append(messages, protected...)
	messages = append(messages, history...)
	messages = append(messages, openai.UserMessage(userMessage))
	return messages
}

func buildSkillTools(skills []*skill.Skill) []openai.ChatCompletionToolUnionParam {
	if len(skills) == 0 {
		return nil
	}

	names := make([]string, 0, len(skills))
	for _, s := range skills {
		names = append(names, s.Name)
	}

	return []openai.ChatCompletionToolUnionParam{{
		OfFunction: &openai.ChatCompletionFunctionToolParam{
			Function: shared.FunctionDefinitionParam{
				Name:        activateSkillToolName,
				Description: openai.String("Load the full instructions for an Agent Skill by name. Use this when the user's request matches a skill from the available skills catalog."),
				Parameters: shared.FunctionParameters{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{
							"type":        "string",
							"description": "The exact skill name from the available skills catalog.",
							"enum":        names,
						},
					},
					"required":             []string{"name"},
					"additionalProperties": false,
				},
				Strict: openai.Bool(true),
			},
		},
	}}
}

func buildSkillCatalogEntries(skills []*skill.Skill) []SkillCatalogEntry {
	entries := make([]SkillCatalogEntry, 0, len(skills))
	for _, s := range skills {
		entries = append(entries, SkillCatalogEntry{
			Name:        s.Name,
			Description: s.Meta.Description,
			Location:    skillLocation(s),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	return entries
}

func (c *Client) catalogSkills() []*skill.Skill {
	allSkills := c.skills.ListSkills()
	skills := make([]*skill.Skill, 0, len(allSkills))
	for _, s := range allSkills {
		if s == nil || !s.IsModelInvocable() || s.Path == "" {
			continue
		}
		skills = append(skills, s)
	}
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})
	return skills
}

func skillLocation(s *skill.Skill) string {
	if s == nil || s.Path == "" {
		return ""
	}
	return absPath(filepath.Join(s.Path, "SKILL.md"))
}

func skillDirectory(s *skill.Skill) string {
	if s == nil || s.Path == "" {
		return ""
	}
	return absPath(s.Path)
}

func absPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func skillResourcePaths(s *skill.Skill) []string {
	if s == nil || s.Resources == nil || s.Path == "" {
		return nil
	}

	files := make([]string, 0, len(s.Resources.Scripts)+len(s.Resources.References)+len(s.Resources.Assets)+len(s.Resources.Templates))
	for _, script := range s.Resources.Scripts {
		files = append(files, relativeSkillPath(s.Path, script.Path))
	}
	for _, ref := range s.Resources.References {
		files = append(files, relativeSkillPath(s.Path, ref.Path))
	}
	for _, asset := range s.Resources.Assets {
		files = append(files, relativeSkillPath(s.Path, asset.Path))
	}
	for _, tmpl := range s.Resources.Templates {
		files = append(files, relativeSkillPath(s.Path, tmpl.Path))
	}

	sort.Strings(files)
	return files
}

func relativeSkillPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func wrapActivatedSkill(activated *ActivatedSkill) string {
	var sb strings.Builder

	sb.WriteString(`<skill_content name="`)
	sb.WriteString(escapeXML(activated.Name))
	sb.WriteString(`">` + "\n")
	if activated.Content != "" {
		sb.WriteString(activated.Content)
		if !strings.HasSuffix(activated.Content, "\n") {
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n")
	sb.WriteString("Skill location: ")
	sb.WriteString(activated.Location)
	sb.WriteString("\n")
	sb.WriteString("Skill directory: ")
	sb.WriteString(activated.Directory)
	sb.WriteString("\n")
	sb.WriteString("Relative paths in this skill are relative to the skill directory.\n")
	if len(activated.Resources) > 0 {
		sb.WriteString("\n<skill_resources>\n")
		for _, resource := range activated.Resources {
			sb.WriteString("  <file>")
			sb.WriteString(escapeXML(resource))
			sb.WriteString("</file>\n")
		}
		sb.WriteString("</skill_resources>\n")
	}
	sb.WriteString("</skill_content>")

	return sb.String()
}

func wrapDuplicateSkillActivation(name string) string {
	return fmt.Sprintf(`<skill_content name="%s">`+"\nSkill %s is already active in this session. Refer to the preserved skill context already in the conversation.\n</skill_content>", escapeXML(name), escapeXML(name))
}

func wrapSkillActivationError(name string, err error) string {
	if name == "" {
		return fmt.Sprintf("<skill_error>%s</skill_error>", escapeXML(err.Error()))
	}
	return fmt.Sprintf(`<skill_error name="%s">%s</skill_error>`, escapeXML(name), escapeXML(err.Error()))
}

func escapeXML(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(s)
}
