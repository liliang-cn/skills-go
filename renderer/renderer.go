package renderer

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Renderer renders skill content with variable substitution and command injection
type Renderer struct {
	timeout time.Duration
}

// RendererOption configures a Renderer
type RendererOption func(*Renderer)

// WithTimeout sets the command execution timeout
func WithTimeout(d time.Duration) RendererOption {
	return func(r *Renderer) {
		r.timeout = d
	}
}

// NewRenderer creates a new renderer
func NewRenderer(opts ...RendererOption) *Renderer {
	r := &Renderer{
		timeout: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Render renders skill content with variable substitution and command injection
func (r *Renderer) Render(ctx context.Context, content string, vars map[string]string) (string, error) {
	// Step 1: Process command injection !`command`
	rendered, err := r.injectCommands(ctx, content)
	if err != nil {
		return "", err
	}

	// Step 2: Process variable substitution
	rendered = r.replaceVariables(rendered, vars)

	return rendered, nil
}

// commandPattern matches !`command` syntax
var commandPattern = regexp.MustCompile("!`([^`]+)`")

// injectCommands processes !`command` syntax
func (r *Renderer) injectCommands(ctx context.Context, content string) (string, error) {
	matches := commandPattern.FindAllStringSubmatch(content, -1)

	result := content
	// Process in reverse order to maintain positions
	for i := len(matches) - 1; i >= 0; i-- {
		match := matches[i]
		placeholder := match[0]
		command := match[1]

		output, err := r.runCommand(ctx, command)
		if err != nil {
			return "", fmt.Errorf("command injection failed: %w", err)
		}

		// Replace only this specific occurrence
		idx := strings.Index(result, placeholder)
		if idx >= 0 {
			result = result[:idx] + output + result[idx+len(placeholder):]
		}
	}

	return result, nil
}

// runCommand executes a shell command
func (r *Renderer) runCommand(ctx context.Context, command string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// Use sh -c to execute command
	cmd := exec.CommandContext(ctx, "sh", "-c", command)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, stderr.String())
	}

	return strings.TrimRight(stdout.String(), "\n"), nil
}

// variablePattern matches variable substitutions
var variablePattern = regexp.MustCompile(`\$(ARGUMENTS(?:\[\d+\])?|\d+|[A-Z_]+|\{[^}]+\})`)

// replaceVariables processes variable substitution
func (r *Renderer) replaceVariables(content string, vars map[string]string) string {
	return variablePattern.ReplaceAllStringFunc(content, func(match string) string {
		// $ARGUMENTS
		if match == "$ARGUMENTS" {
			return vars["ARGUMENTS"]
		}

		// $ARGUMENTS[N]
		if strings.HasPrefix(match, "$ARGUMENTS[") {
			idx := extractIndex(match)
			vals := getArgumentList(vars)
			if idx < len(vals) {
				return vals[idx]
			}
			return ""
		}

		// $N (numbers)
		if strings.HasPrefix(match, "$") {
			rest := match[1:]
			if idx, err := parseIndex(rest); err == nil {
				vals := getArgumentList(vars)
				if idx < len(vals) {
					return vals[idx]
				}
			}
		}

		// ${VAR}
		if strings.HasPrefix(match, "${") && strings.HasSuffix(match, "}") {
			varName := match[2 : len(match)-1]
			if val, ok := vars[varName]; ok {
				return val
			}
			// Try with CLAUDE_ prefix
			if val, ok := vars["CLAUDE_"+varName]; ok {
				return val
			}
			return ""
		}

		// $VAR
		varName := match[1:]
		if val, ok := vars[varName]; ok {
			return val
		}
		// Try with CLAUDE_ prefix
		if val, ok := vars["CLAUDE_"+varName]; ok {
			return val
		}

		return match
	})
}

// extractIndex extracts index from $ARGUMENTS[N]
func extractIndex(s string) int {
	start := strings.Index(s, "[") + 1
	end := strings.Index(s, "]")
	if start > 0 && end > start {
		var idx int
		fmt.Sscanf(s[start:end], "%d", &idx)
		return idx
	}
	return 0
}

// parseIndex parses a string as an integer index
func parseIndex(s string) (int, error) {
	var idx int
	_, err := fmt.Sscanf(s, "%d", &idx)
	return idx, err
}

// BuildVars builds variables map from invocation context
func BuildVars(ctx *InvocationContext) map[string]string {
	vars := make(map[string]string)

	if ctx == nil {
		return vars
	}

	// ARGUMENTS as space-separated string
	vars["ARGUMENTS"] = strings.Join(ctx.Arguments, " ")

	// ARGUMENTS_LIST as string (null-separated for later splitting)
	if len(ctx.Arguments) > 0 {
		vars["ARGUMENTS_LIST"] = strings.Join(ctx.Arguments, "\x00")
	}

	// Individual arguments
	for i, arg := range ctx.Arguments {
		vars[fmt.Sprintf("%d", i)] = arg
	}

	// Session ID
	vars["SESSION_ID"] = ctx.SessionID
	vars["CLAUDE_SESSION_ID"] = ctx.SessionID

	// Environment variables
	for k, v := range ctx.Environment {
		vars[k] = v
	}

	// Custom variables
	for k, v := range ctx.Variables {
		vars[k] = v
	}

	// User ID
	if ctx.UserID != "" {
		vars["USER_ID"] = ctx.UserID
	}

	return vars
}

// getArgumentList extracts argument list from vars
func getArgumentList(vars map[string]string) []string {
	if vals, ok := vars["ARGUMENTS_LIST"]; ok {
		return strings.Split(vals, "\x00")
	}
	return []string{}
}

// InvocationContext represents the context when invoking a skill
type InvocationContext struct {
	Arguments   []string
	SessionID   string
	Environment map[string]string
	Variables   map[string]string
	UserID      string
}
