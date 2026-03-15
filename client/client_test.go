package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liliang-cn/skills-go/skill"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name  string
		cfg   *Config
		check func(*Client) error
	}{
		{
			name: "nil config uses defaults",
			cfg:  nil,
			check: func(c *Client) error {
				if c == nil {
					return tError("client is nil")
				}
				if c.model != "gpt-4o" {
					return tErrorf("default model = %q, want gpt-4o", c.model)
				}
				return nil
			},
		},
		{
			name: "empty config uses defaults",
			cfg:  &Config{},
			check: func(c *Client) error {
				if c.model != "gpt-4o" {
					return tErrorf("default model = %q, want gpt-4o", c.model)
				}
				return nil
			},
		},
		{
			name: "custom model",
			cfg: &Config{
				Model: "gpt-4-turbo",
			},
			check: func(c *Client) error {
				if c.model != "gpt-4-turbo" {
					return tErrorf("model = %q, want gpt-4-turbo", c.model)
				}
				return nil
			},
		},
		{
			name: "custom base URL",
			cfg: &Config{
				BaseURL: "https://api.example.com",
			},
			check: func(c *Client) error {
				// Base URL is passed to OpenAI client, we can't directly inspect it
				// but we verify the client was created
				if c == nil {
					return tError("client is nil")
				}
				return nil
			},
		},
		{
			name: "with skill paths",
			cfg: &Config{
				SkillPaths: []string{"./skills", "/usr/share/skills"},
			},
			check: func(c *Client) error {
				if c == nil {
					return tError("client is nil")
				}
				return nil
			},
		},
		{
			name: "with timeout",
			cfg: &Config{
				Timeout: 60,
			},
			check: func(c *Client) error {
				if c == nil {
					return tError("client is nil")
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewClient(tt.cfg)
			if err := tt.check(c); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestClientGetSkill(t *testing.T) {
	cfg := &Config{
		SkillPaths: []string{"testdata/skills"},
	}

	c := NewClient(cfg)

	// Add a test skill directly
	testSkill := &skill.Skill{
		Name: "test-skill",
		Path: "/path/to/test",
		Meta: skill.Meta{Name: "test-skill", Description: "A test skill"},
	}
	c.skills.Add(testSkill)

	// Test Get
	retrieved, err := c.GetSkill("test-skill")
	if err != nil {
		t.Fatalf("GetSkill failed: %v", err)
	}
	if retrieved.Name != "test-skill" {
		t.Errorf("got name %q, want %q", retrieved.Name, "test-skill")
	}

	// Test Get with non-existent skill
	_, err = c.GetSkill("nonexistent")
	if err != skill.ErrSkillNotFound {
		t.Errorf("expected ErrSkillNotFound, got %v", err)
	}
}

func TestClientGetSkillWithLevelLoadsContentOnDemand(t *testing.T) {
	baseDir := t.TempDir()
	skillPath := filepath.Join(baseDir, "test-skill")

	if err := os.MkdirAll(skillPath, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte(`---
name: test-skill
description: Exposes client-side progressive disclosure.
---

Full skill instructions.
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	c := NewClient(&Config{SkillPaths: []string{baseDir}})
	ctx := context.Background()

	if err := c.LoadSkills(ctx); err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}

	metaOnly, err := c.GetSkill("test-skill")
	if err != nil {
		t.Fatalf("GetSkill failed: %v", err)
	}
	if metaOnly.Content != "" {
		t.Fatalf("expected metadata-only skill before upgrade, got content %q", metaOnly.Content)
	}

	full, err := c.GetSkillWithLevel(ctx, "test-skill", skill.LoadLevelContent)
	if err != nil {
		t.Fatalf("GetSkillWithLevel failed: %v", err)
	}
	if full.Content != "Full skill instructions." {
		t.Fatalf("Content = %q, want full skill instructions", full.Content)
	}
	if full.LoadLevel != skill.LoadLevelContent {
		t.Fatalf("LoadLevel = %v, want %v", full.LoadLevel, skill.LoadLevelContent)
	}
}

func TestClientSkillCatalogIncludesLocation(t *testing.T) {
	baseDir := t.TempDir()
	skillPath := filepath.Join(baseDir, "catalog-skill")

	if err := os.MkdirAll(skillPath, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte(`---
name: catalog-skill
description: Included in the model-visible catalog.
---

Catalog content.
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	c := NewClient(&Config{SkillPaths: []string{baseDir}})
	if err := c.LoadSkills(context.Background()); err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}

	catalog := c.SkillCatalog()
	wantLocation, err := filepath.Abs(filepath.Join(skillPath, "SKILL.md"))
	if err != nil {
		t.Fatalf("Abs failed: %v", err)
	}

	entry, ok := findCatalogEntry(catalog, "catalog-skill")
	if !ok {
		t.Fatalf("catalog missing %q: %#v", "catalog-skill", catalog)
	}
	if entry.Location != wantLocation {
		t.Fatalf("Location = %q, want %q", entry.Location, wantLocation)
	}
}

func TestClientActivateSkillWrapsContentAndResources(t *testing.T) {
	baseDir := t.TempDir()
	skillPath := filepath.Join(baseDir, "wrapped-skill")

	if err := os.MkdirAll(filepath.Join(skillPath, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(skillPath, "references"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(skillPath, "assets"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte(`---
name: wrapped-skill
description: Returns structured activation content.
---

Use this wrapped skill.
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "scripts", "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "references", "guide.md"), []byte("guide"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "assets", "sample.txt"), []byte("asset"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "template.md"), []byte("template"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	c := NewClient(&Config{SkillPaths: []string{baseDir}})
	ctx := context.Background()
	if err := c.LoadSkills(ctx); err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}

	activated, err := c.ActivateSkill(ctx, "wrapped-skill")
	if err != nil {
		t.Fatalf("ActivateSkill failed: %v", err)
	}

	if activated.Content != "Use this wrapped skill." {
		t.Fatalf("Content = %q, want wrapped body", activated.Content)
	}
	if len(activated.Resources) != 4 {
		t.Fatalf("Resources = %d, want 4", len(activated.Resources))
	}
	for _, want := range []string{"scripts/run.sh", "references/guide.md", "assets/sample.txt", "template.md"} {
		if !containsString(activated.Resources, want) {
			t.Fatalf("Resources missing %q: %#v", want, activated.Resources)
		}
	}
	for _, want := range []string{
		`<skill_content name="wrapped-skill">`,
		"Skill directory: ",
		"<skill_resources>",
		"<file>scripts/run.sh</file>",
	} {
		if !strings.Contains(activated.Wrapped, want) {
			t.Fatalf("Wrapped output missing %q:\n%s", want, activated.Wrapped)
		}
	}
}

func TestClientListSkills(t *testing.T) {
	cfg := &Config{}
	c := NewClient(cfg)

	// Add test skills
	for i := 0; i < 3; i++ {
		c.skills.Add(&skill.Skill{
			Name: string(rune('a' + i)),
			Path: "/path/" + string(rune('a'+i)),
			Meta: skill.Meta{Name: string(rune('a' + i))},
		})
	}

	metas := c.ListSkills()
	if len(metas) != 3 {
		t.Errorf("ListSkills returned %d items, want 3", len(metas))
	}
}

func TestClientListSkillNames(t *testing.T) {
	cfg := &Config{}
	c := NewClient(cfg)

	// Add test skills
	names := []string{"alpha", "beta", "gamma"}
	for _, name := range names {
		c.skills.Add(&skill.Skill{
			Name: name,
			Path: "/path/" + name,
			Meta: skill.Meta{Name: name},
		})
	}

	result := c.ListSkillNames()
	if len(result) != 3 {
		t.Errorf("ListSkillNames returned %d items, want 3", len(result))
	}

	// Convert to set for comparison
	nameSet := make(map[string]bool)
	for _, n := range result {
		nameSet[n] = true
	}

	for _, expected := range names {
		if !nameSet[expected] {
			t.Errorf("missing name %q", expected)
		}
	}
}

func TestClientReloadSkills(t *testing.T) {
	cfg := &Config{
		SkillPaths: []string{"testdata/skills"},
	}
	c := NewClient(cfg)

	ctx := context.Background()

	// Add a skill
	c.skills.Add(&skill.Skill{
		Name: "test",
		Path: "/path/test",
		Meta: skill.Meta{Name: "test"},
	})

	// Reload should clear and reload (even if testdata doesn't exist, it shouldn't crash)
	err := c.ReloadSkills(ctx)
	if err != nil {
		// May fail if testdata doesn't exist, that's ok
		t.Logf("ReloadSkills returned error (expected if testdata missing): %v", err)
	}
}

func TestClientListScripts(t *testing.T) {
	cfg := &Config{}
	c := NewClient(cfg)

	// Add a skill with scripts
	testSkill := &skill.Skill{
		Name: "test",
		Path: "/path/test",
		Meta: skill.Meta{Name: "test"},
		Resources: &skill.Resources{
			Scripts: []skill.Script{
				{Name: "analyze", Path: "/scripts/analyze.py", Language: "python"},
				{Name: "test", Path: "/scripts/test.sh", Language: "bash"},
			},
		},
	}
	c.skills.Add(testSkill)

	scripts, err := c.ListScripts("test")
	if err != nil {
		t.Fatalf("ListScripts failed: %v", err)
	}

	if len(scripts) != 2 {
		t.Errorf("ListScripts returned %d items, want 2", len(scripts))
	}

	// Check script names
	names := make(map[string]bool)
	for _, s := range scripts {
		names[s.Name] = true
	}

	for _, expected := range []string{"analyze", "test"} {
		if !names[expected] {
			t.Errorf("missing script %q", expected)
		}
	}
}

func TestClientListScriptsNotFound(t *testing.T) {
	cfg := &Config{}
	c := NewClient(cfg)

	_, err := c.ListScripts("nonexistent")
	if err != skill.ErrSkillNotFound {
		t.Errorf("expected ErrSkillNotFound, got %v", err)
	}
}

func TestClientListScriptsLoadsResourcesOnDemand(t *testing.T) {
	baseDir := t.TempDir()
	skillPath := filepath.Join(baseDir, "script-skill")

	if err := os.MkdirAll(filepath.Join(skillPath, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte(`---
name: script-skill
description: Lists scripts from a metadata-first load.
---
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "scripts", "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	c := NewClient(&Config{SkillPaths: []string{baseDir}})
	if err := c.LoadSkills(context.Background()); err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}

	scripts, err := c.ListScripts("script-skill")
	if err != nil {
		t.Fatalf("ListScripts failed: %v", err)
	}
	if len(scripts) != 1 {
		t.Fatalf("ListScripts returned %d scripts, want 1", len(scripts))
	}
	if scripts[0].Name != "run" {
		t.Fatalf("script name = %q, want %q", scripts[0].Name, "run")
	}
}

func TestClientDefaultSkillOutputDir(t *testing.T) {
	c := NewClient(&Config{})
	if got := c.defaultSkillOutputDir(); got != ".agents/skills" {
		t.Fatalf("defaultSkillOutputDir = %q, want %q", got, ".agents/skills")
	}
}

func TestClientExecuteShell(t *testing.T) {
	cfg := &Config{}
	c := NewClient(cfg)

	ctx := context.Background()
	result, err := c.ExecuteShell(ctx, "echo 'test output'")

	if err != nil && result.Error == nil {
		t.Fatalf("ExecuteShell failed: %v", err)
	}

	if result == nil {
		t.Fatal("result is nil")
	}

	// The output should contain "test output"
	if result.Stdout != "" && result.Stdout != "test output" {
		t.Logf("Stdout = %q (may have trailing newlines)", result.Stdout)
	}
}

func TestClientExecuteScriptNotFound(t *testing.T) {
	cfg := &Config{}
	c := NewClient(cfg)

	ctx := context.Background()
	_, err := c.ExecuteScript(ctx, "nonexistent", "script")

	if err == nil {
		t.Error("expected error for nonexistent skill")
	}
}

func TestClientExecuteScriptNoResources(t *testing.T) {
	cfg := &Config{}
	c := NewClient(cfg)

	// Add a skill without resources
	c.skills.Add(&skill.Skill{
		Name: "no-resources",
		Path: "/path/no-resources",
		Meta: skill.Meta{Name: "no-resources"},
	})

	ctx := context.Background()
	_, err := c.ExecuteScript(ctx, "no-resources", "script")

	if err == nil {
		t.Error("expected error for skill with no resources")
	}
}

func TestClientExecuteScriptNotFoundInSkill(t *testing.T) {
	cfg := &Config{}
	c := NewClient(cfg)

	// Add a skill with resources but no matching script
	c.skills.Add(&skill.Skill{
		Name: "test",
		Path: "/path/test",
		Meta: skill.Meta{Name: "test"},
		Resources: &skill.Resources{
			Scripts: []skill.Script{
				{Name: "other", Path: "/other.sh", Language: "bash"},
			},
		},
	})

	ctx := context.Background()
	_, err := c.ExecuteScript(ctx, "test", "nonexistent")

	if err == nil {
		t.Error("expected error for nonexistent script")
	}
}

func TestClientSetScriptTimeout(t *testing.T) {
	cfg := &Config{}
	c := NewClient(cfg)

	c.SetScriptTimeout(10)

	// Just verify it doesn't crash - the timeout is set internally
	if c.executor == nil {
		t.Error("executor should not be nil after SetScriptTimeout")
	}
}

func TestClientExecuteScriptPath(t *testing.T) {
	cfg := &Config{}
	c := NewClient(cfg)

	ctx := context.Background()

	// Execute a simple shell command via path
	result, err := c.ExecuteScriptPath(ctx, "/bin/echo", "hello")

	if err != nil && result.Error == nil {
		t.Logf("ExecuteScriptPath may fail on systems without /bin/echo: %v", err)
	}

	if result != nil && result.Stdout != "" {
		t.Logf("Output: %s", result.Stdout)
	}
}

func TestClientLoadSkillsRespectsProjectTrust(t *testing.T) {
	homeDir := t.TempDir()
	projectDir := t.TempDir()
	setClientSkillEnv(t, homeDir, projectDir)

	projectSkillPath := filepath.Join(projectDir, ".agents", "skills", "blocked-skill")
	if err := os.MkdirAll(projectSkillPath, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectSkillPath, "SKILL.md"), []byte(`---
name: blocked-skill
description: Project skill should be blocked by trust policy.
---
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	c := NewClient(&Config{}, WithProjectSkillsTrusted(false))
	if err := c.LoadSkills(context.Background()); err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}

	if _, err := c.GetSkill("blocked-skill"); err != skill.ErrSkillNotFound {
		t.Fatalf("GetSkill error = %v, want %v", err, skill.ErrSkillNotFound)
	}

	diagnostics := c.SkillDiagnostics()
	if len(diagnostics) != 1 {
		t.Fatalf("SkillDiagnostics returned %d items, want 1", len(diagnostics))
	}
	if diagnostics[0].Code != "untrusted_project_skill" {
		t.Fatalf("Diagnostic code = %q, want %q", diagnostics[0].Code, "untrusted_project_skill")
	}
}

func TestChatConfigDefaults(t *testing.T) {
	cfg := &chatConfig{}

	if cfg.SessionID != "" {
		cfg.SessionID = "" // Reset for test
	}

	// Verify defaults can be set
	cfg.SessionID = "test-session"
	if cfg.SessionID != "test-session" {
		t.Error("SessionID not set correctly")
	}
}

func TestClientChatUsesActivateSkillTool(t *testing.T) {
	baseDir := t.TempDir()
	skillPath := filepath.Join(baseDir, "tool-skill")

	if err := os.MkdirAll(filepath.Join(skillPath, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte(`---
name: tool-skill
description: Use when the user asks for tool-assisted guidance.
---

Follow the tool skill instructions.
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "scripts", "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode failed: %v", err)
		}

		messages, ok := body["messages"].([]any)
		if !ok {
			t.Fatalf("messages missing or invalid: %#v", body["messages"])
		}

		switch requestCount {
		case 1:
			if _, ok := body["tools"].([]any); !ok {
				t.Fatalf("expected tools in first request: %#v", body["tools"])
			}
			if !messagesContainText(messages, "<available_skills>") {
				t.Fatalf("first request missing skills catalog: %#v", messages)
			}
			if !messagesContainText(messages, "tool-skill/SKILL.md") {
				t.Fatalf("first request missing skill location: %#v", messages)
			}

			writeJSON(t, w, map[string]any{
				"id":      "chatcmpl-1",
				"object":  "chat.completion",
				"created": 1,
				"model":   "gpt-4o",
				"choices": []any{
					map[string]any{
						"index":         0,
						"finish_reason": "tool_calls",
						"message": map[string]any{
							"role":    "assistant",
							"content": "",
							"tool_calls": []any{
								map[string]any{
									"id":   "call_1",
									"type": "function",
									"function": map[string]any{
										"name":      activateSkillToolName,
										"arguments": `{"name":"tool-skill"}`,
									},
								},
							},
						},
					},
				},
				"usage": map[string]any{
					"prompt_tokens":     10,
					"completion_tokens": 5,
					"total_tokens":      15,
				},
			})
		case 2:
			if !messagesContainRole(messages, "tool") {
				t.Fatalf("second request missing tool message: %#v", messages)
			}
			if !messagesContainText(messages, `<skill_content name="tool-skill">`) {
				t.Fatalf("second request missing activated skill payload: %#v", messages)
			}
			if !messagesContainText(messages, "<file>scripts/run.sh</file>") {
				t.Fatalf("second request missing resource listing: %#v", messages)
			}

			writeJSON(t, w, map[string]any{
				"id":      "chatcmpl-2",
				"object":  "chat.completion",
				"created": 2,
				"model":   "gpt-4o",
				"choices": []any{
					map[string]any{
						"index":         0,
						"finish_reason": "stop",
						"message": map[string]any{
							"role":    "assistant",
							"content": "Activated the requested skill.",
						},
					},
				},
				"usage": map[string]any{
					"prompt_tokens":     12,
					"completion_tokens": 4,
					"total_tokens":      16,
				},
			})
		default:
			t.Fatalf("unexpected request %d", requestCount)
		}
	}))
	defer server.Close()

	c := NewClient(&Config{
		APIKey:     "test-key",
		BaseURL:    server.URL,
		Model:      "gpt-4o",
		SkillPaths: []string{baseDir},
	})
	if err := c.LoadSkills(context.Background()); err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}

	resp, err := c.Chat(context.Background(), "Please use the tool skill for this task.")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if resp.Content != "Activated the requested skill." {
		t.Fatalf("Content = %q, want %q", resp.Content, "Activated the requested skill.")
	}
	if len(resp.SkillsUsed) != 1 || resp.SkillsUsed[0] != "tool-skill" {
		t.Fatalf("SkillsUsed = %#v, want [tool-skill]", resp.SkillsUsed)
	}
	if resp.FinishReason != "stop" {
		t.Fatalf("FinishReason = %q, want %q", resp.FinishReason, "stop")
	}
	if resp.Usage.TotalTokens != 31 {
		t.Fatalf("TotalTokens = %d, want %d", resp.Usage.TotalTokens, 31)
	}
	if requestCount != 2 {
		t.Fatalf("requestCount = %d, want 2", requestCount)
	}
}

func TestClientChatReplaysSessionSkillsAndDedupsActivation(t *testing.T) {
	baseDir := t.TempDir()
	skillPath := filepath.Join(baseDir, "sticky-skill")

	if err := os.MkdirAll(filepath.Join(skillPath, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte(`---
name: sticky-skill
description: Persists across a session once activated.
---

Keep using this sticky skill.
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "scripts", "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode failed: %v", err)
		}
		messages, ok := body["messages"].([]any)
		if !ok {
			t.Fatalf("messages missing or invalid: %#v", body["messages"])
		}

		switch requestCount {
		case 1:
			if messagesContainText(messages, "remains in effect for this session") {
				t.Fatalf("unexpected preserved skill context on first turn: %#v", messages)
			}
			writeJSON(t, w, map[string]any{
				"id":      "chatcmpl-s1",
				"object":  "chat.completion",
				"created": 1,
				"model":   "gpt-4o",
				"choices": []any{
					map[string]any{
						"index":         0,
						"finish_reason": "tool_calls",
						"message": map[string]any{
							"role":    "assistant",
							"content": "",
							"tool_calls": []any{
								map[string]any{
									"id":   "call_s1",
									"type": "function",
									"function": map[string]any{
										"name":      activateSkillToolName,
										"arguments": `{"name":"sticky-skill"}`,
									},
								},
							},
						},
					},
				},
				"usage": map[string]any{
					"prompt_tokens":     10,
					"completion_tokens": 5,
					"total_tokens":      15,
				},
			})
		case 2:
			if !messagesContainText(messages, `<skill_content name="sticky-skill">`) {
				t.Fatalf("first turn missing activated skill payload: %#v", messages)
			}
			writeJSON(t, w, map[string]any{
				"id":      "chatcmpl-s2",
				"object":  "chat.completion",
				"created": 2,
				"model":   "gpt-4o",
				"choices": []any{
					map[string]any{
						"index":         0,
						"finish_reason": "stop",
						"message": map[string]any{
							"role":    "assistant",
							"content": "First activation complete.",
						},
					},
				},
				"usage": map[string]any{
					"prompt_tokens":     11,
					"completion_tokens": 4,
					"total_tokens":      15,
				},
			})
		case 3:
			if !messagesContainText(messages, "remains in effect for this session") {
				t.Fatalf("second turn missing preserved session skill context: %#v", messages)
			}
			if !messagesContainText(messages, `<skill_content name="sticky-skill">`) {
				t.Fatalf("second turn missing preserved wrapped skill: %#v", messages)
			}
			writeJSON(t, w, map[string]any{
				"id":      "chatcmpl-s3",
				"object":  "chat.completion",
				"created": 3,
				"model":   "gpt-4o",
				"choices": []any{
					map[string]any{
						"index":         0,
						"finish_reason": "tool_calls",
						"message": map[string]any{
							"role":    "assistant",
							"content": "",
							"tool_calls": []any{
								map[string]any{
									"id":   "call_s2",
									"type": "function",
									"function": map[string]any{
										"name":      activateSkillToolName,
										"arguments": `{"name":"sticky-skill"}`,
									},
								},
							},
						},
					},
				},
				"usage": map[string]any{
					"prompt_tokens":     12,
					"completion_tokens": 5,
					"total_tokens":      17,
				},
			})
		case 4:
			if !messagesContainText(messages, "already active in this session") {
				t.Fatalf("duplicate activation was not deduped: %#v", messages)
			}
			writeJSON(t, w, map[string]any{
				"id":      "chatcmpl-s4",
				"object":  "chat.completion",
				"created": 4,
				"model":   "gpt-4o",
				"choices": []any{
					map[string]any{
						"index":         0,
						"finish_reason": "stop",
						"message": map[string]any{
							"role":    "assistant",
							"content": "Reused preserved session skill.",
						},
					},
				},
				"usage": map[string]any{
					"prompt_tokens":     9,
					"completion_tokens": 3,
					"total_tokens":      12,
				},
			})
		default:
			t.Fatalf("unexpected request %d", requestCount)
		}
	}))
	defer server.Close()

	c := NewClient(&Config{
		APIKey:     "test-key",
		BaseURL:    server.URL,
		Model:      "gpt-4o",
		SkillPaths: []string{baseDir},
	})
	if err := c.LoadSkills(context.Background()); err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}

	resp1, err := c.Chat(context.Background(), "Activate the sticky skill.", WithSessionID("session-1"))
	if err != nil {
		t.Fatalf("first Chat failed: %v", err)
	}
	if len(resp1.SkillsUsed) != 1 || resp1.SkillsUsed[0] != "sticky-skill" {
		t.Fatalf("first SkillsUsed = %#v, want [sticky-skill]", resp1.SkillsUsed)
	}

	active := c.ActiveSessionSkills("session-1")
	if len(active) != 1 || active[0].Name != "sticky-skill" {
		t.Fatalf("ActiveSessionSkills = %#v, want sticky-skill", active)
	}

	resp2, err := c.Chat(context.Background(), "Use it again.", WithSessionID("session-1"))
	if err != nil {
		t.Fatalf("second Chat failed: %v", err)
	}
	if len(resp2.SkillsUsed) != 0 {
		t.Fatalf("second SkillsUsed = %#v, want no new activations", resp2.SkillsUsed)
	}
	if resp2.Content != "Reused preserved session skill." {
		t.Fatalf("second Content = %q, want reuse response", resp2.Content)
	}
	if requestCount != 4 {
		t.Fatalf("requestCount = %d, want 4", requestCount)
	}

	c.ClearSessionSkills("session-1")
	if got := c.ActiveSessionSkills("session-1"); len(got) != 0 {
		t.Fatalf("ActiveSessionSkills after clear = %#v, want empty", got)
	}
}

// Helper functions for testing

func writeJSON(t *testing.T, w http.ResponseWriter, payload map[string]any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
}

func messagesContainText(messages []any, want string) bool {
	for _, message := range messages {
		msgMap, ok := message.(map[string]any)
		if !ok {
			continue
		}
		content, ok := msgMap["content"].(string)
		if ok && strings.Contains(content, want) {
			return true
		}
	}
	return false
}

func messagesContainRole(messages []any, role string) bool {
	for _, message := range messages {
		msgMap, ok := message.(map[string]any)
		if !ok {
			continue
		}
		if gotRole, ok := msgMap["role"].(string); ok && gotRole == role {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func setClientSkillEnv(t *testing.T, homeDir, projectDir string) {
	t.Helper()
	t.Setenv("HOME", homeDir)

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevWD)
	})
}

func findCatalogEntry(entries []SkillCatalogEntry, name string) (SkillCatalogEntry, bool) {
	for _, entry := range entries {
		if entry.Name == name {
			return entry, true
		}
	}
	return SkillCatalogEntry{}, false
}

type testError string

func (e testError) Error() string {
	return string(e)
}

func tError(msg string) error {
	return testError(msg)
}

func tErrorf(format string, args ...interface{}) error {
	return testError(format)
}
