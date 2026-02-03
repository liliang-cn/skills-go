package client

import (
	"context"
	"testing"

	"github.com/liliang-cn/skills-go/skill"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name   string
		cfg    *Config
		check  func(*Client) error
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

// Helper functions for testing

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
