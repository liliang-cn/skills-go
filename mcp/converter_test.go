package mcp

import (
	"strings"
	"testing"
)

func TestFormatSkillMDUsesMinimalFrontmatter(t *testing.T) {
	c := NewConverter()
	out := c.formatSkillMD("sample-skill", "A sample skill", "Body")

	if !strings.Contains(out, "name: sample-skill") {
		t.Fatalf("formatted skill missing name: %s", out)
	}
	if !strings.Contains(out, "description: A sample skill") {
		t.Fatalf("formatted skill missing description: %s", out)
	}
	if strings.Contains(out, "user-invocable") {
		t.Fatalf("formatted skill should not include extension field by default: %s", out)
	}
}

func TestSanitizeNameProducesSpecFriendlyNames(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "My MCP Server", want: "my-mcp-server"},
		{input: "my__server", want: "my-server"},
		{input: "---bad---name---", want: "bad-name"},
		{input: strings.Repeat("a", 80), want: strings.Repeat("a", 64)},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := sanitizeName(tt.input); got != tt.want {
				t.Fatalf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
