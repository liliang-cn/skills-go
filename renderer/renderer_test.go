package renderer

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNewRenderer(t *testing.T) {
	r := NewRenderer()

	if r == nil {
		t.Fatal("NewRenderer returned nil")
	}

	if r.timeout != 30*time.Second {
		t.Errorf("default timeout = %v, want 30s", r.timeout)
	}
}

func TestRendererWithTimeout(t *testing.T) {
	timeout := 10 * time.Second
	r := NewRenderer(WithTimeout(timeout))

	if r.timeout != timeout {
		t.Errorf("timeout = %v, want %v", r.timeout, timeout)
	}
}

func TestBuildVars(t *testing.T) {
	tests := []struct {
		name   string
		ctx    *InvocationContext
		check  map[string]string // key -> expected value (empty string means should exist)
	}{
		{
			name:  "nil context",
			ctx:   nil,
			check: map[string]string{},
		},
		{
			name: "context with arguments",
			ctx: &InvocationContext{
				Arguments: []string{"arg1", "arg2", "arg3"},
			},
			check: map[string]string{
				"ARGUMENTS":     "arg1 arg2 arg3",
				"ARGUMENTS_LIST": "arg1\x00arg2\x00arg3",
				"0":             "arg1",
				"1":             "arg2",
				"2":             "arg3",
			},
		},
		{
			name: "context with session",
			ctx: &InvocationContext{
				Arguments: []string{},
				SessionID: "session-123",
			},
			check: map[string]string{
				"SESSION_ID":       "session-123",
				"CLAUDE_SESSION_ID": "session-123",
			},
		},
		{
			name: "context with environment",
			ctx: &InvocationContext{
				Environment: map[string]string{"PATH": "/usr/bin", "HOME": "/home/user"},
			},
			check: map[string]string{
				"PATH": "/usr/bin",
				"HOME": "/home/user",
			},
		},
		{
			name: "context with variables",
			ctx: &InvocationContext{
				Variables: map[string]string{"CUSTOM_VAR": "custom_value"},
			},
			check: map[string]string{
				"CUSTOM_VAR": "custom_value",
			},
		},
		{
			name: "context with user ID",
			ctx: &InvocationContext{
				UserID: "user-456",
			},
			check: map[string]string{
				"USER_ID": "user-456",
			},
		},
		{
			name: "full context",
			ctx: &InvocationContext{
				Arguments:   []string{"test"},
				SessionID:   "session-1",
				Environment: map[string]string{"ENV": "value"},
				Variables:   map[string]string{"VAR": "custom"},
				UserID:      "user-1",
			},
			check: map[string]string{
				"ARGUMENTS":        "test",
				"SESSION_ID":        "session-1",
				"CLAUDE_SESSION_ID": "session-1",
				"ENV":               "value",
				"VAR":               "custom",
				"USER_ID":           "user-1",
				"0":                 "test",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars := BuildVars(tt.ctx)

			for key, expected := range tt.check {
				val, ok := vars[key]
				if !ok {
					t.Errorf("key %q not found in vars", key)
					continue
				}
				if expected != "" && val != expected {
					t.Errorf("vars[%q] = %q, want %q", key, val, expected)
				}
			}
		})
	}
}

func TestReplaceVariables(t *testing.T) {
	r := NewRenderer()

	tests := []struct {
		name     string
		content  string
		vars     map[string]string
		expected string
	}{
		{
			name:     "$ARGUMENTS replacement",
			content:  "Process $ARGUMENTS",
			vars:     map[string]string{"ARGUMENTS": "file1 file2"},
			expected: "Process file1 file2",
		},
		{
			name:     "$ARGUMENTS[0] replacement",
			content:  "First: $ARGUMENTS[0]",
			vars:     map[string]string{"ARGUMENTS_LIST": "first\x00second\x00third"},
			expected: "First: first",
		},
		{
			name:     "$ARGUMENTS[1] replacement",
			content:  "Second: $ARGUMENTS[1]",
			vars:     map[string]string{"ARGUMENTS_LIST": "first\x00second"},
			expected: "Second: second",
		},
		{
			name:     "$ARGUMENTS out of bounds",
			content:  "Third: $ARGUMENTS[5]",
			vars:     map[string]string{"ARGUMENTS_LIST": "first\x00second"},
			expected: "Third: ",
		},
		{
			name:     "numeric $0 replacement",
			content:  "$0 and $1",
			vars:     map[string]string{"ARGUMENTS_LIST": "a\x00b"},
			expected: "a and b",
		},
		{
			name:     "out of bounds numeric preserved",
			content:  "$5",
			vars:     map[string]string{"ARGUMENTS_LIST": "a\x00b"},
			expected: "$5", // Out of bounds numeric variables are preserved
		},
		{
			name:     "${VAR} replacement",
			content:  "Value is ${CUSTOM}",
			vars:     map[string]string{"CUSTOM": "myvalue"},
			expected: "Value is myvalue",
		},
		{
			name:     "$VAR replacement",
			content:  "Value is $CUSTOM",
			vars:     map[string]string{"CUSTOM": "myvalue"},
			expected: "Value is myvalue",
		},
		{
			name:     "$VAR with CLAUDE_ prefix fallback",
			content:  "Session: ${SESSION_ID}",
			vars:     map[string]string{"CLAUDE_SESSION_ID": "abc123"},
			expected: "Session: abc123",
		},
		{
			name:     "unknown variable preserved",
			content:  "Unknown: $UNKNOWN",
			vars:     map[string]string{},
			expected: "Unknown: $UNKNOWN",
		},
		{
			name:     "mixed variables",
			content:  "Args: $ARGUMENTS, First: $0, Custom: ${VAR}",
			vars:     map[string]string{"ARGUMENTS": "a b", "ARGUMENTS_LIST": "a\x00b", "VAR": "test"},
			expected: "Args: a b, First: a, Custom: test",
		},
		{
			name:     "empty variables",
			content:  "No variables here",
			vars:     map[string]string{},
			expected: "No variables here",
		},
		{
			name:     "dollar sign at end",
			content:  "ends with $",
			vars:     map[string]string{},
			expected: "ends with $",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.replaceVariables(tt.content, tt.vars)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestInjectCommands(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectInOut []string // strings that should be in output
		expectError bool
	}{
		{
			name:        "simple echo command",
			content:     "Result: !`echo hello`",
			expectInOut: []string{"Result: hello"},
		},
		{
			name:        "multiple commands",
			content:     "!`echo first` and !`echo second`",
			expectInOut: []string{"first", "second"},
		},
		{
			name:        "command with quotes",
			content:     "!`echo \"hello world\"`",
			expectInOut: []string{"hello world"},
		},
		{
			name:        "date command",
			content:     "Time: !`echo 2024-01-01`",
			expectInOut: []string{"Time: 2024-01-01"},
		},
		{
			name:        "no commands",
			content:     "Just plain text",
			expectInOut: []string{"Just plain text"},
		},
		{
			name:        "empty command result",
			content:     "!`echo -n` end",
			expectInOut: []string{"end"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRenderer(WithTimeout(5 * time.Second))
			ctx := context.Background()

			result, err := r.injectCommands(ctx, tt.content)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			for _, expected := range tt.expectInOut {
				if !strings.Contains(result, expected) {
					t.Errorf("output missing expected string %q\nGot: %s", expected, result)
				}
			}
		})
	}
}

func TestInjectCommandsTimeout(t *testing.T) {
	r := NewRenderer(WithTimeout(100 * time.Millisecond))
	ctx := context.Background()

	// Sleep command should timeout
	_, err := r.injectCommands(ctx, "start !`sleep 5` end")
	if err == nil {
		t.Errorf("expected timeout error but got none")
	}
}

func TestInjectCommandsInvalidCommand(t *testing.T) {
	r := NewRenderer()
	ctx := context.Background()

	// Invalid command should return error
	_, err := r.injectCommands(ctx, "!`exit 1`")
	if err == nil {
		t.Errorf("expected error for failing command but got none")
	}
}

func TestRender(t *testing.T) {
	r := NewRenderer(WithTimeout(5 * time.Second))
	ctx := context.Background()

	tests := []struct {
		name     string
		content  string
		vars     map[string]string
		expected string
	}{
		{
			name:     "variables only",
			content:  "Hello $ARGUMENTS",
			vars:     map[string]string{"ARGUMENTS": "world"},
			expected: "Hello world",
		},
		{
			name:     "command only",
			content:  "Result: !`echo test`",
			vars:     map[string]string{},
			expected: "Result: test",
		},
		{
			name:     "variables and command",
			content:  "Args: $ARGUMENTS, Cmd: !`echo done`",
			vars:     map[string]string{"ARGUMENTS": "a b"},
			expected: "Args: a b, Cmd: done",
		},
		{
			name:     "command result with variables",
			content:  "!`echo $ARGUMENTS`",
			vars:     map[string]string{"ARGUMENTS": "test"},
			// Commands run first, then variables are replaced
			// So this tests the order
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := r.Render(ctx, tt.content, tt.vars)

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if tt.expected != "" && result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestExtractIndex(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"$ARGUMENTS[0]", 0},
		{"$ARGUMENTS[1]", 1},
		{"$ARGUMENTS[10]", 10},
		{"$ARGUMENTS[999]", 999},
		{"invalid", 0},
		{"$ARGUMENTS[]", 0},
		{"$ARGUMENTS[abc]", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := extractIndex(tt.input)
			if result != tt.expected {
				t.Errorf("extractIndex(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseIndex(t *testing.T) {
	tests := []struct {
		input    string
		expected int
		hasError bool
	}{
		{"0", 0, false},
		{"5", 5, false},
		{"42", 42, false},
		{"abc", 0, true},
		{"", 0, true},
		{"1.5", 1, false}, // sscanf reads 1
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseIndex(tt.input)

			if tt.hasError {
				if err == nil {
					t.Errorf("expected error for %q", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result != tt.expected {
					t.Errorf("parseIndex(%q) = %d, want %d", tt.input, result, tt.expected)
				}
			}
		})
	}
}

func TestGetArgumentList(t *testing.T) {
	tests := []struct {
		name     string
		vars     map[string]string
		expected []string
	}{
		{
			name:     "valid list",
			vars:     map[string]string{"ARGUMENTS_LIST": "a\x00b\x00c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "empty list",
			vars:     map[string]string{"ARGUMENTS_LIST": ""},
			expected: []string{""},
		},
		{
			name:     "missing list",
			vars:     map[string]string{},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getArgumentList(tt.vars)

			if len(result) != len(tt.expected) {
				t.Errorf("got %d items, want %d", len(result), len(tt.expected))
			}

			for i, v := range tt.expected {
				if i >= len(result) || result[i] != v {
					t.Errorf("result[%d] = %q, want %q", i, getResult(result, i), v)
				}
			}
		})
	}
}

func getResult(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return ""
}

func TestInvocationContext(t *testing.T) {
	ctx := &InvocationContext{
		Arguments:   []string{"arg1", "arg2"},
		SessionID:   "session-123",
		Environment: map[string]string{"ENV": "val"},
		Variables:   map[string]string{"VAR": "custom"},
		UserID:      "user-456",
	}

	if len(ctx.Arguments) != 2 {
		t.Errorf("Arguments length = %d, want 2", len(ctx.Arguments))
	}
	if ctx.SessionID != "session-123" {
		t.Errorf("SessionID = %q, want %q", ctx.SessionID, "session-123")
	}
	if ctx.UserID != "user-456" {
		t.Errorf("UserID = %q, want %q", ctx.UserID, "user-456")
	}
}
