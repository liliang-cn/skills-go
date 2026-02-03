package config

import (
	"os"
	"path/filepath"
)

// Config holds the configuration for the skills client
type Config struct {
	// APIKey is the OpenAI API key
	APIKey string

	// BaseURL is the base URL for the API (optional)
	BaseURL string

	// Model is the default model to use
	Model string

	// SkillPaths are the paths to search for skills
	SkillPaths []string

	// Timeout is the request timeout in seconds
	Timeout int
}

// LoadFromEnv loads configuration from environment variables
func LoadFromEnv() *Config {
	cfg := &Config{
		APIKey:  os.Getenv("OPENAI_API_KEY"),
		BaseURL: os.Getenv("OPENAI_BASE_URL"),
		Model:   os.Getenv("OPENAI_MODEL"),
	}

	if cfg.Model == "" {
		cfg.Model = "gpt-4o"
	}

	// Set default skill paths
	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		homeDir = os.Getenv("USERPROFILE")
	}

	cfg.SkillPaths = []string{
		".claude/skills",
		filepath.Join(homeDir, ".claude/skills"),
	}

	// Add custom paths from env
	if customPath := os.Getenv("SKILLS_PATH"); customPath != "" {
		cfg.SkillPaths = append(cfg.SkillPaths, customPath)
	}

	return cfg
}

// LoadFromFile loads configuration from a file
func LoadFromFile(path string) (*Config, error) {
	// TODO: Implement YAML/JSON config file loading
	return LoadFromEnv(), nil
}

// Merge merges two configs, with cfg2 taking precedence
func (cfg *Config) Merge(cfg2 *Config) *Config {
	if cfg == nil {
		return cfg2
	}
	if cfg2 == nil {
		return cfg
	}

	result := *cfg

	if cfg2.APIKey != "" {
		result.APIKey = cfg2.APIKey
	}
	if cfg2.BaseURL != "" {
		result.BaseURL = cfg2.BaseURL
	}
	if cfg2.Model != "" {
		result.Model = cfg2.Model
	}
	if cfg2.SkillPaths != nil {
		result.SkillPaths = append(result.SkillPaths, cfg2.SkillPaths...)
	}
	if cfg2.Timeout > 0 {
		result.Timeout = cfg2.Timeout
	}

	return &result
}

// Validate validates the configuration
func (cfg *Config) Validate() error {
	if cfg.APIKey == "" {
		return &ValidationError{Field: "APIKey", Message: "API key is required"}
	}
	return nil
}

// ValidationError is returned when configuration is invalid
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
