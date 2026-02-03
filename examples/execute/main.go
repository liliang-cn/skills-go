package main

import (
	"context"
	"fmt"
	"log"

	"github.com/liliang-cn/skills-go/client"
	"github.com/liliang-cn/skills-go/skill"
)

func main() {
	cfg := &client.Config{
		APIKey:     "sk-test", // 测试用，不需要真实 key
		Model:      "gpt-4o",
		SkillPaths: []string{"../skills"},
	}

	cli := client.NewClient(cfg)

	// Load skills
	ctx := context.Background()
	if err := cli.LoadSkills(ctx); err != nil {
		log.Printf("Warning: failed to load skills: %v", err)
	}

	// List available scripts
	fmt.Println("=== Available Scripts ===")
	scripts, _ := cli.ListScripts("commit")
	for _, s := range scripts {
		fmt.Printf("  %s (%s) at %s\n", s.Name, s.Language, s.Path)
	}
	fmt.Println()

	// Execute a shell command
	fmt.Println("=== Execute Shell Command ===")
	result, err := cli.ExecuteShell(ctx, "echo 'Hello from skills-go!'")
	if err != nil {
		log.Printf("Command error: %v", err)
	} else {
		fmt.Printf("Stdout: %s\n", result.Stdout)
		fmt.Printf("Duration: %v\n", result.Duration)
	}
	fmt.Println()

	// Execute a script by name
	fmt.Println("=== Execute Script by Name ===")
	result, err = cli.ExecuteScript(ctx, "commit", "analyze")
	if err != nil {
		log.Printf("Script error: %v", err)
	} else {
		fmt.Printf("Stdout: %s\n", result.Stdout)
		if result.Stderr != "" {
			fmt.Printf("Stderr: %s\n", result.Stderr)
		}
		fmt.Printf("Exit Code: %d\n", result.ExitCode)
		fmt.Printf("Duration: %v\n", result.Duration)
	}
	fmt.Println()

	// Show executor usage directly
	fmt.Println("=== Direct Executor Usage ===")
	executor := skill.NewExecutor(
		skill.WithTimeout(10),
	)

	result, err = executor.ExecuteShell(ctx, "date")
	if err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Printf("Current time: %s\n", result.Stdout)
	}
}
