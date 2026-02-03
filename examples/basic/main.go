package main

import (
	"context"
	"fmt"
	"log"

	"github.com/liliang-cn/skills-go/client"
	"github.com/liliang-cn/skills-go/config"
)

func main() {
	// Load config from environment
	cfg := config.LoadFromEnv()

	// Override with your API key if needed
	if cfg.APIKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// Create client
	cli := client.NewClient(&client.Config{
		APIKey:     cfg.APIKey,
		BaseURL:    cfg.BaseURL,
		Model:      cfg.Model,
		SkillPaths: cfg.SkillPaths,
	})

	// Load skills
	ctx := context.Background()
	if err := cli.LoadSkills(ctx); err != nil {
		log.Printf("Warning: failed to load skills: %v", err)
	}

	// List available skills
	fmt.Println("Available skills:")
	for _, name := range cli.ListSkillNames() {
		skill, _ := cli.GetSkill(name)
		fmt.Printf("  /%s: %s\n", name, skill.Meta.Description)
	}
	fmt.Println()

	// Example 1: Regular chat with automatic skill matching
	fmt.Println("=== Example 1: Regular chat ===")
	resp, err := cli.Chat(ctx, "How does authentication work in this codebase?")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Response: %s\n", resp.Content)
	fmt.Printf("Skills used: %v\n", resp.SkillsUsed)
	fmt.Printf("Tokens: %d\n", resp.Usage.TotalTokens)
	fmt.Println()

	// Example 2: Direct skill invocation
	fmt.Println("=== Example 2: Direct skill invocation ===")
	resp, err = cli.Chat(ctx, "/explain-code main.go",
		client.WithAsUser(true),
		client.WithSessionID("test-session"),
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Response: %s\n", resp.Content)
	fmt.Printf("Skills used: %v\n", resp.SkillsUsed)
	fmt.Println()

	// Example 3: Skill with arguments
	fmt.Println("=== Example 3: Skill with arguments ===")
	resp, err = cli.Chat(ctx, "/fix-issue 123",
		client.WithAsUser(true),
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Response: %s\n", resp.Content)
}
