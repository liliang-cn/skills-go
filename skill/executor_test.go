package skill

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNewExecutor(t *testing.T) {
	exec := NewExecutor()

	if exec == nil {
		t.Fatal("NewExecutor returned nil")
	}

	if exec.timeout != 30*time.Second {
		t.Errorf("default timeout = %v, want 30s", exec.timeout)
	}
}

func TestExecutorWithTimeout(t *testing.T) {
	timeout := 5 * time.Second
	exec := NewExecutor(WithTimeout(timeout))

	if exec.timeout != timeout {
		t.Errorf("timeout = %v, want %v", exec.timeout, timeout)
	}
}

func TestExecutorWithEnv(t *testing.T) {
	env := []string{"TEST_VAR=value", "ANOTHER=one"}
	exec := NewExecutor(WithEnv(env))

	if len(exec.env) != 2 {
		t.Errorf("env length = %d, want 2", len(exec.env))
	}
}

func TestExecutorExecuteShell(t *testing.T) {
	exec := NewExecutor(WithTimeout(5 * time.Second))
	ctx := context.Background()

	result, err := exec.ExecuteShell(ctx, "echo 'hello world'")

	// ExecuteShell returns result, nil even on error (error is in result.Error)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if !strings.Contains(result.Stdout, "hello world") {
		t.Errorf("Stdout = %q, want to contain 'hello world'", result.Stdout)
	}

	if result.Duration == 0 {
		t.Errorf("Duration was not recorded")
	}
}

func TestExecutorExecuteShellExitCode(t *testing.T) {
	exec := NewExecutor(WithTimeout(5 * time.Second))
	ctx := context.Background()

	result, _ := exec.ExecuteShell(ctx, "exit 42")

	// Exit code is stored in result, not returned as error
	if result.ExitCode != 42 {
		t.Errorf("ExitCode = %d, want 42", result.ExitCode)
	}

	// Error should be in result.Error
	if result.Error == nil {
		t.Errorf("expected Error to be set for non-zero exit code")
	}
}

func TestFindScript(t *testing.T) {
	skill := &Skill{
		Name: "test",
		Resources: &Resources{
			Scripts: []Script{
				{Name: "analyze", Path: "/scripts/analyze.py", Language: "python"},
				{Name: "test", Path: "/scripts/test.sh", Language: "bash"},
			},
		},
	}

	tests := []struct {
		name        string
		scriptName  string
		expectFound bool
		expectLang  string
	}{
		{"found analyze script", "analyze", true, "python"},
		{"found test script", "test", true, "bash"},
		{"not found", "nonexistent", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := FindScript(skill, tt.scriptName)

			if tt.expectFound {
				if script == nil {
					t.Errorf("expected to find script %q", tt.scriptName)
				} else if script.Language != tt.expectLang {
					t.Errorf("script language = %q, want %q", script.Language, tt.expectLang)
				}
			} else {
				if script != nil {
					t.Errorf("expected nil but got script: %+v", script)
				}
			}
		})
	}
}

func TestListScripts(t *testing.T) {
	tests := []struct {
		name          string
		skill         *Skill
		expectedCount int
	}{
		{"nil resources", &Skill{}, 0},
		{"with scripts", &Skill{
			Resources: &Resources{
				Scripts: []Script{
					{Name: "a", Path: "/a"},
					{Name: "b", Path: "/b"},
				},
			},
		}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scripts := ListScripts(tt.skill)
			if len(scripts) != tt.expectedCount {
				t.Errorf("got %d scripts, want %d", len(scripts), tt.expectedCount)
			}
		})
	}
}

func TestExecutorExecute(t *testing.T) {
	exec := NewExecutor()
	ctx := context.Background()

	// Skill with no resources
	skill1 := &Skill{Name: "test", Path: "/path/test"}
	_, err := exec.Execute(ctx, skill1, "script")
	if err == nil {
		t.Error("expected error for skill with no resources")
	}

	// Script not found
	skill2 := &Skill{
		Name: "test",
		Path: "/path/test",
		Resources: &Resources{
			Scripts: []Script{{Name: "other", Path: "/other.sh", Language: "bash"}},
		},
	}
	_, err = exec.Execute(ctx, skill2, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent script")
	}
}
