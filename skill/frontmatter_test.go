package skill

import (
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		expectedName    string
		expectedDesc    string
		expectedContent string
		expectError     bool
	}{
		{
			name: "valid frontmatter with all fields",
			input: `---
name: test-skill
description: A test skill
user-invocable: true
---
This is the content.`,
			expectedName:    "test-skill",
			expectedDesc:    "A test skill",
			expectedContent: "This is the content.",
		},
		{
			name: "frontmatter without description preserves empty description",
			input: `---
name: test-skill
---
First paragraph.

Second paragraph.`,
			expectedName:    "test-skill",
			expectedDesc:    "",
			expectedContent: "First paragraph.\n\nSecond paragraph.",
		},
		{
			name:        "no frontmatter returns error",
			input:       `Just content without frontmatter.`,
			expectError: true,
		},
		{
			name: "invalid frontmatter missing closing separator",
			input: `---
name: test
Only content`,
			expectError: true,
		},
		{
			name: "frontmatter with array fields",
			input: `---
name: test
allowed-tools:
  - Read
  - Write
---
Content here.`,
			expectedName:    "test",
			expectedDesc:    "",
			expectedContent: "Content here.",
		},
		{
			name: "frontmatter with comma-separated allowed-tools",
			input: `---
name: test
allowed-tools: "Read, Write, Edit, Bash"
---
Content here.`,
			expectedName:    "test",
			expectedDesc:    "",
			expectedContent: "Content here.",
		},
		{
			name: "frontmatter with space-delimited allowed-tools (official spec)",
			input: `---
name: test
allowed-tools: "Bash(git:*) Bash(jq:*) Read"
---
Content here.`,
			expectedName:    "test",
			expectedDesc:    "",
			expectedContent: "Content here.",
		},
		{
			name: "frontmatter with metadata field",
			input: `---
name: test
metadata:
  version: "1.0.0"
  author: someone
---
Content.`,
			expectedName:    "test",
			expectedDesc:    "",
			expectedContent: "Content.",
		},
		{
			name: "frontmatter with PreToolUse hooks",
			input: `---
name: test
hooks:
  PreToolUse:
    - matcher: "Write|Edit"
      hooks:
        - type: command
          command: "echo test"
---
Content.`,
			expectedName:    "test",
			expectedDesc:    "",
			expectedContent: "Content.",
		},
		{
			name: "empty content",
			input: `---
name: empty-skill
---
`,
			expectedName:    "empty-skill",
			expectedDesc:    "",
			expectedContent: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, content, err := ParseFrontmatter([]byte(tt.input))

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if meta.Name != tt.expectedName {
				t.Errorf("Name = %q, want %q", meta.Name, tt.expectedName)
			}

			if meta.Description != tt.expectedDesc {
				t.Errorf("Description = %q, want %q", meta.Description, tt.expectedDesc)
			}

			if content != tt.expectedContent {
				t.Errorf("Content = %q, want %q", content, tt.expectedContent)
			}
		})
	}
}

func TestMarshalFrontmatter(t *testing.T) {
	tests := []struct {
		name     string
		meta     *Meta
		content  string
		contains []string // substrings that should be in output
	}{
		{
			name: "basic metadata",
			meta: &Meta{
				Name:        "test-skill",
				Description: "A test skill",
			},
			content:  "Skill content here.",
			contains: []string{"name: test-skill", "description: A test skill", "Skill content here."},
		},
		{
			name: "metadata with allowed tools",
			meta: &Meta{
				Name:         "test",
				AllowedTools: []string{"Read", "Write", "Edit"},
			},
			content:  "Content",
			contains: []string{"- Read", "- Write", "- Edit"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := MarshalFrontmatter(tt.meta, tt.content)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			outputStr := string(output)
			for _, expected := range tt.contains {
				if !contains(outputStr, expected) {
					t.Errorf("output missing expected substring %q\nGot: %s", expected, outputStr)
				}
			}

			// Verify it starts with ---
			if !startsWith(outputStr, "---\n") {
				t.Errorf("output should start with ---\\n, got: %s", outputStr[:10])
			}
		})
	}
}

func TestParseArguments(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedSkill string
		expectedArgs  []string
		expectEmpty   bool
	}{
		{
			name:          "valid skill invocation",
			input:         "/commit help me",
			expectedSkill: "commit",
			expectedArgs:  []string{"help", "me"},
		},
		{
			name:          "skill with no args",
			input:         "/commit",
			expectedSkill: "commit",
			expectedArgs:  nil,
		},
		{
			name:        "missing slash",
			input:       "commit help",
			expectEmpty: true,
		},
		{
			name:        "empty string",
			input:       "",
			expectEmpty: true,
		},
		{
			name:        "only slash",
			input:       "/",
			expectEmpty: true,
		},
		{
			name:          "slash with spaces",
			input:         "  /skill-name  arg1  arg2  ",
			expectedSkill: "skill-name",
			expectedArgs:  []string{"arg1", "arg2"},
		},
		{
			name:          "skill with dash",
			input:         "/my-skill arg1",
			expectedSkill: "my-skill",
			expectedArgs:  []string{"arg1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			skill, args := ParseArguments(tt.input)

			if tt.expectEmpty {
				if skill != "" || args != nil {
					t.Errorf("expected empty result, got skill=%q args=%v", skill, args)
				}
				return
			}

			if skill != tt.expectedSkill {
				t.Errorf("skill = %q, want %q", skill, tt.expectedSkill)
			}

			if !equalSlices(args, tt.expectedArgs) {
				t.Errorf("args = %v, want %v", args, tt.expectedArgs)
			}
		})
	}
}

// Helper functions

func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr) >= 0
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func starts(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
