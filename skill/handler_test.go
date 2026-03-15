package skill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisterFunction(t *testing.T) {
	registry := NewRegistry(nil)

	// Register a simple handler
	handler := func(ctx context.Context, vars map[string]interface{}) (string, error) {
		name, _ := vars["name"].(string)
		return "Hello, " + name + "!", nil
	}

	registry.RegisterFunction("greet", "Greet a person by name", handler)

	// Verify it's registered
	if !registry.IsHandlerSkill("greet") {
		t.Error("expected greet to be a handler skill")
	}

	// Get the skill
	skill, err := registry.Get("greet")
	if err != nil {
		t.Fatalf("failed to get skill: %v", err)
	}

	if skill.Name != "greet" {
		t.Errorf("expected name 'greet', got %s", skill.Name)
	}

	if skill.Meta.Description != "Greet a person by name" {
		t.Errorf("unexpected description: %s", skill.Meta.Description)
	}
}

func TestInvokeHandler(t *testing.T) {
	registry := NewRegistry(nil)

	// Register a handler
	handler := func(ctx context.Context, vars map[string]interface{}) (string, error) {
		name, _ := vars["name"].(string)
		if name == "" {
			name = "World"
		}
		return "Hello, " + name + "!", nil
	}

	registry.RegisterFunction("greet", "Greet a person", handler)

	// Test invocation
	ctx := context.Background()
	output, err := InvokeWithVariables(ctx, registry, "greet", map[string]interface{}{
		"name": "Alice",
	})
	if err != nil {
		t.Fatalf("invocation failed: %v", err)
	}

	if output != "Hello, Alice!" {
		t.Errorf("unexpected output: %s", output)
	}

	// Test with default
	output, err = InvokeWithVariables(ctx, registry, "greet", map[string]interface{}{})
	if err != nil {
		t.Fatalf("invocation failed: %v", err)
	}

	if output != "Hello, World!" {
		t.Errorf("unexpected output: %s", output)
	}
}

func TestInvokeFileBasedSkill(t *testing.T) {
	registry := NewRegistry(nil)

	// Add a file-based skill (without handler)
	skill := &Skill{
		Name:    "test-skill",
		Path:    "/test/skill.md",
		Content: "This is the skill content",
		Meta: Meta{
			Name:        "test-skill",
			Description: "A test skill",
		},
	}
	registry.Add(skill)

	// Invoke should return the content
	ctx := context.Background()
	outcome, err := Invoke(ctx, registry, "test-skill", ExecutionOptions{})
	if err != nil {
		t.Fatalf("invocation failed: %v", err)
	}

	if !outcome.Success {
		t.Error("expected success")
	}

	if outcome.Output != "This is the skill content" {
		t.Errorf("unexpected output: %s", outcome.Output)
	}
}

func TestInvokeFileBasedSkillLoadsContentOnDemand(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()
	skillPath := filepath.Join(baseDir, "test-skill")

	if err := os.MkdirAll(skillPath, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte(`---
name: test-skill
description: Loads file content lazily when invoked.
---

This is the lazily loaded skill content.
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	loader := NewLoader(WithPaths(baseDir))
	registry := NewRegistry(loader)

	if err := registry.Load(ctx); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	skill, err := registry.Get("test-skill")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if skill.Content != "" {
		t.Fatalf("expected metadata-only skill before invoke, got content %q", skill.Content)
	}

	outcome, err := Invoke(ctx, registry, "test-skill", ExecutionOptions{})
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	if !outcome.Success {
		t.Fatal("expected success")
	}
	if outcome.Output != "This is the lazily loaded skill content." {
		t.Fatalf("Output = %q, want lazily loaded content", outcome.Output)
	}
	if skill.LoadLevel != LoadLevelContent {
		t.Fatalf("LoadLevel = %v, want %v", skill.LoadLevel, LoadLevelContent)
	}
}

func TestHandlerSkill(t *testing.T) {
	// Create a HandlerSkill directly
	handlerSkill := NewHandlerSkill("calculator", "Performs calculations", func(ctx context.Context, vars map[string]interface{}) (string, error) {
		// Handle both int and float64 types
		var a, b int
		if af, ok := vars["a"].(float64); ok {
			a = int(af)
		} else if ai, ok := vars["a"].(int); ok {
			a = ai
		}
		if bf, ok := vars["b"].(float64); ok {
			b = int(bf)
		} else if bi, ok := vars["b"].(int); ok {
			b = bi
		}
		return "Result: " + intToString(a+b), nil
	})

	if !handlerSkill.IsHandlerSkill() {
		t.Error("expected IsHandlerSkill to be true")
	}

	if handlerSkill.GetHandler() == nil {
		t.Error("expected GetHandler to return non-nil")
	}

	// Register and use it
	registry := NewRegistry(nil)
	registry.RegisterHandlerSkill(handlerSkill)

	ctx := context.Background()
	output, err := InvokeWithVariables(ctx, registry, "calculator", map[string]interface{}{
		"a": 5,
		"b": 3,
	})
	if err != nil {
		t.Fatalf("invocation failed: %v", err)
	}

	if !strings.Contains(output, "8") {
		t.Errorf("expected result 8, got: %s", output)
	}
}

func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	var negative bool
	if n < 0 {
		negative = true
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if negative {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}
