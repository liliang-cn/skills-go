package skill

import (
	"context"
	"fmt"
	"time"
)

// ExecutionOptions configures skill execution
type ExecutionOptions struct {
	Variables   map[string]interface{}
	Environment map[string]string
	SessionID   string
	UserID      string
	IsUser      bool
}

// ExecutionOutcome represents the result of executing a skill
type ExecutionOutcome struct {
	Success   bool                   `json:"success"`
	Output    string                 `json:"output"`
	Error     string                 `json:"error,omitempty"`
	Duration  time.Duration          `json:"duration"`
	Metadata  map[string]string      `json:"metadata,omitempty"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

// Invoke executes a skill, handling both file-based and handler-based skills
func Invoke(ctx context.Context, registry *Registry, name string, opts ExecutionOptions) (*ExecutionOutcome, error) {
	start := time.Now()

	// Get the skill
	skill, err := registry.Get(name)
	if err != nil {
		return &ExecutionOutcome{
			Success:  false,
			Error:    err.Error(),
			Duration: time.Since(start),
		}, err
	}

	// Check if it's a handler skill
	if handler := registry.GetHandler(name); handler != nil {
		return executeHandler(ctx, handler, opts, start)
	}

	// For file-based skills, return the rendered content
	return &ExecutionOutcome{
		Success:  true,
		Output:   skill.Content,
		Duration: time.Since(start),
		Metadata: map[string]string{
			"skill_name": skill.Name,
			"skill_path": skill.Path,
		},
	}, nil
}

// executeHandler executes a Go function handler
func executeHandler(ctx context.Context, handler HandlerFunc, opts ExecutionOptions, start time.Time) (*ExecutionOutcome, error) {
	output, err := handler(ctx, opts.Variables)
	if err != nil {
		return &ExecutionOutcome{
			Success:  false,
			Error:    err.Error(),
			Duration: time.Since(start),
		}, err
	}

	return &ExecutionOutcome{
		Success:  true,
		Output:   output,
		Duration: time.Since(start),
		Variables: opts.Variables,
	}, nil
}

// InvokeWithVariables is a convenience method for simple skill invocation
func InvokeWithVariables(ctx context.Context, registry *Registry, name string, vars map[string]interface{}) (string, error) {
	outcome, err := Invoke(ctx, registry, name, ExecutionOptions{
		Variables: vars,
	})
	if err != nil {
		return "", err
	}
	if !outcome.Success {
		return "", fmt.Errorf("skill execution failed: %s", outcome.Error)
	}
	return outcome.Output, nil
}
