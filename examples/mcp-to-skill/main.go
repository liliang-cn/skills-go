package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/liliang-cn/skills-go/mcp"
	"github.com/liliang-cn/skills-go/skill"
)

func main() {
	ctx := context.Background()

	if len(os.Args) < 2 {
		printUsage()
		return
	}

	command := os.Args[1]

	switch command {
	case "convert":
		handleConvert(ctx, os.Args[2:], false)
	case "convert-http":
		handleConvertHTTP(ctx, os.Args[2:], false)
	case "convert-llm":
		handleConvert(ctx, os.Args[2:], true)
	case "convert-http-llm":
		handleConvertHTTP(ctx, os.Args[2:], true)
	case "discover":
		handleDiscover(ctx, os.Args[2:])
	case "help":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n\n", command)
		printUsage()
	}
}

func printUsage() {
	fmt.Println("MCP to Skill Converter")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  go run main.go convert <command> <args...>        Convert MCP server to skill")
	fmt.Println("  go run main.go convert-http <url>                Convert HTTP MCP server to skill")
	fmt.Println("  go run main.go convert-llm <command> <args...>   Convert using LLM (enhanced)")
	fmt.Println("  go run main.go convert-http-llm <url>            Convert HTTP using LLM")
	fmt.Println("  go run main.go discover <command> <args...>      Discover MCP server capabilities")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  # Convert a Python MCP server")
	fmt.Println("  go run main.go convert python server.py")
	fmt.Println()
	fmt.Println("  # Convert a Node.js MCP server")
	fmt.Println("  go run main.go convert node server.js -- --arg value")
	fmt.Println()
	fmt.Println("  # Convert an HTTP MCP server")
	fmt.Println("  go run main.go convert-http http://localhost:38476/sse")
	fmt.Println()
	fmt.Println("  # Convert using LLM for enhanced descriptions")
	fmt.Println("  OPENAI_API_KEY=sk-xxx go run main.go convert-llm python server.py")
	fmt.Println()
	fmt.Println("  # Discover capabilities")
	fmt.Println("  go run main.go discover python server.py")
	fmt.Println()
	fmt.Println("Environment Variables:")
	fmt.Println("  SKILLS_OUTPUT_DIR    Output directory for generated skills (default: ./.agents/skills)")
	fmt.Println("  OPENAI_API_KEY       OpenAI API key for LLM-based conversion")
	fmt.Println("  OPENAI_BASE_URL      Optional base URL for OpenAI-compatible API")
}

func handleConvert(ctx context.Context, args []string, useLLM bool) {
	if len(args) < 1 {
		log.Fatal("convert requires at least a command")
	}

	// Get output directory
	outputDir := os.Getenv("SKILLS_OUTPUT_DIR")
	if outputDir == "" {
		outputDir = ".agents/skills"
	}

	// Create server config
	cfg := &mcp.ServerConfig{
		Command: args,
		Include: mcp.DefaultInclude(),
	}

	// Convert
	var result *skill.Skill
	var err error

	if useLLM {
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			log.Fatal("OPENAI_API_KEY environment variable is required for LLM conversion")
		}
		baseURL := os.Getenv("OPENAI_BASE_URL")
		converter := mcp.NewConverter(mcp.WithLLM(apiKey, baseURL))
		result, err = converter.ConvertWithLLM(ctx, cfg, outputDir)
	} else {
		converter := mcp.NewConverter()
		result, err = converter.Convert(ctx, cfg, outputDir)
	}

	if err != nil {
		log.Fatalf("Failed to convert: %v", err)
	}

	fmt.Printf("Successfully converted MCP server to skill!\n")
	fmt.Printf("Skill name: %s\n", result.Name)
	fmt.Printf("Skill path: %s\n", result.Path)
	if useLLM {
		fmt.Println("(Enhanced with LLM)")
	}
	fmt.Println()
	fmt.Println("The skill has been added to your skills directory.")
	fmt.Println("Restart your application to use the new skill.")
}

func handleConvertHTTP(ctx context.Context, args []string, useLLM bool) {
	if len(args) < 1 {
		log.Fatal("convert-http requires a URL")
	}

	url := args[0]

	// Get output directory
	outputDir := os.Getenv("SKILLS_OUTPUT_DIR")
	if outputDir == "" {
		outputDir = ".agents/skills"
	}

	// Create server config
	cfg := &mcp.ServerConfig{
		URL:     url,
		Include: mcp.DefaultInclude(),
	}

	// Convert
	var result *skill.Skill
	var err error

	if useLLM {
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			log.Fatal("OPENAI_API_KEY environment variable is required for LLM conversion")
		}
		baseURL := os.Getenv("OPENAI_BASE_URL")
		converter := mcp.NewConverter(mcp.WithLLM(apiKey, baseURL))
		result, err = converter.ConvertWithLLM(ctx, cfg, outputDir)
	} else {
		converter := mcp.NewConverter()
		result, err = converter.Convert(ctx, cfg, outputDir)
	}

	if err != nil {
		log.Fatalf("Failed to convert: %v", err)
	}

	fmt.Printf("Successfully converted MCP server to skill!\n")
	fmt.Printf("Skill name: %s\n", result.Name)
	fmt.Printf("Skill path: %s\n", result.Path)
	if useLLM {
		fmt.Println("(Enhanced with LLM)")
	}
	fmt.Println()
	fmt.Println("The skill has been added to your skills directory.")
	fmt.Println("Restart your application to use the new skill.")
}

func handleDiscover(ctx context.Context, args []string) {
	if len(args) < 1 {
		log.Fatal("discover requires at least a command")
	}

	// Create server config
	cfg := &mcp.ServerConfig{
		Command: args,
		Include: mcp.DefaultInclude(),
	}

	// Discover
	converter := mcp.NewConverter()
	caps, err := converter.Discover(ctx, cfg)
	if err != nil {
		log.Fatalf("Failed to discover: %v", err)
	}

	fmt.Println("MCP Server Capabilities")
	fmt.Println("======================")
	fmt.Println()

	if tools := caps.ListTools(); len(tools) > 0 {
		fmt.Printf("Tools (%d):\n", len(tools))
		for _, name := range tools {
			fmt.Printf("  - %s\n", name)
		}
		fmt.Println()
	}

	if resources := caps.ListResources(); len(resources) > 0 {
		fmt.Printf("Resources (%d):\n", len(resources))
		for _, uri := range resources {
			fmt.Printf("  - %s\n", uri)
		}
		fmt.Println()
	}

	if prompts := caps.ListPrompts(); len(prompts) > 0 {
		fmt.Printf("Prompts (%d):\n", len(prompts))
		for _, name := range prompts {
			fmt.Printf("  - %s\n", name)
		}
		fmt.Println()
	}
}
