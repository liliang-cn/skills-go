# skills-go

A Go library for loading and using Agent Skills with OpenAI API (v3.17.0). Includes MCP (Model Context Protocol) server to Skill conversion.

## Features

- **Skill Loading**: Load skills from any location (personal, project, plugin)
- **Progressive Disclosure**: Metadata (Level 1) → Content (Level 2) → Full Resources (Level 3)
- **Interactive Execution**: Built-in PTY and Pipe support via `pipeit` for interactive scripts
- **Variable Substitution**: `$ARGUMENTS`, `$N`, `${CLAUDE_SESSION_ID}`
- **Command Injection**: Dynamic context with `!command` syntax
- **Fork Mode**: Run skills in isolated subagent contexts
- **MCP Integration**: Convert MCP servers to Skills with optional LLM enhancement
- **Extensible**: Inject your own OpenAI client or use as a standalone skill manager
- **Agent Skills Defaults**: Metadata-first discovery, `.agents/skills` defaults, and spec-aware validation
- **Tool-Driven Activation**: Built-in `activate_skill` flow with skill catalog disclosure and structured activation payloads
- **Discovery Diagnostics**: Collision warnings and trust-gating diagnostics are surfaced without failing skill loading
- **Extensions**: Supports additional frontmatter fields for local runtime features

## Installation

```bash
go get github.com/liliang-cn/skills-go
```

## MCP to Skill Conversion

Convert MCP (Model Context Protocol) servers to Agent Skills with support for both stdio and HTTP transports.

### Basic Conversion (No LLM)

Fast, deterministic conversion that preserves original MCP tool descriptions:

```go
import "github.com/liliang-cn/skills-go/mcp"

converter := mcp.NewConverter()

cfg := &mcp.ServerConfig{
    Name:    "fetch",
    Command: []string{"npx", "-y", "@modelcontextprotocol/server-fetch"},
    Include: mcp.DefaultInclude(),  // tools, resources, prompts
}

skill, err := converter.Convert(ctx, cfg, "./skills")
```

### LLM-Enhanced Conversion

Use LLM to generate user-friendly documentation with examples and better descriptions:

```go
import (
    "github.com/liliang-cn/skills-go/mcp"
    "github.com/openai/openai-go/v3"
    "github.com/openai/openai-go/v3/option"
)

llmClient := openai.NewClient(
    option.WithAPIKey("sk-..."),
    option.WithBaseURL("http://localhost:11434/v1"),  // Optional: Ollama
)

converter := mcp.NewConverter(
    mcp.WithLLMClient(llmClient),
    mcp.WithLLMModel("qwen3:8b"),  // Optional: defaults to gpt-4o
)

skill, err := converter.ConvertWithLLM(ctx, cfg, "./skills")
```

### Using API Key Directly

```go
converter := mcp.NewConverter(
    mcp.WithLLM("sk-...", "http://localhost:11434/v1"),
)
skill, err := converter.ConvertWithLLM(ctx, cfg, "./skills")
```

### HTTP-based MCP Servers

```go
cfg := &mcp.ServerConfig{
    Name: "my-server",
    URL:  "http://localhost:38476/sse",
    Include: mcp.DefaultInclude(),
}
skill, err := converter.Convert(ctx, cfg, "./skills")
```

### Quick Convert Functions

```go
// Command-based server
skill, err := mcp.QuickConvert(ctx, "python", "server.py", "./skills")

// HTTP server
skill, err := mcp.QuickConvertHTTP(ctx, "http://localhost:38476/sse", "./skills")

// With LLM
skill, err := mcp.QuickConvertWithLLMAPIKey(ctx, "sk-...", "python", "server.py", "./skills")
```

### Discover Capabilities Without Converting

```go
converter := mcp.NewConverter()
caps, err := converter.Discover(ctx, cfg)

fmt.Println("Tools:", caps.ListTools())
fmt.Println("Resources:", caps.ListResources())
fmt.Println("Prompts:", caps.ListPrompts())
```

### Runtime MCP Server Connection

Connect to MCP servers at runtime and call their tools:

```go
import "github.com/liliang-cn/skills-go/client"

cli := client.NewClient(&client.Config{APIKey: "..."})

// Connect to an MCP server
srv, err := cli.ConnectMCPServer(ctx, &mcp.ServerConfig{
    Name:    "fetch",
    Command: []string{"npx", "-y", "@modelcontextprotocol/server-fetch"},
})

// Call a tool
result, err := srv.CallTool(ctx, "fetch", map[string]any{
    "url": "https://example.com",
})

// Read a resource
data, err := srv.ReadResource(ctx, "file:///path/to/file.txt")

// Get a prompt
prompt, metadata, err := srv.GetPrompt(ctx, "summary", nil)
```

### Client Integration

The skills-go client has built-in MCP support:

```go
cli := client.NewClient(&client.Config{
    APIKey: os.Getenv("OPENAI_API_KEY"),
})

// Convert MCP server to skill
skill, err := cli.ConvertMCPServerCommand(ctx, "fetch", "npx", "-y", "@modelcontextprotocol/server-fetch")

// With LLM enhancement
skill, err := cli.ConvertMCPServerCommandWithLLM(ctx, "fetch", "npx", "-y", "@modelcontextprotocol/server-fetch")
```

### CLI Tool

A built-in CLI tool for quick conversions:

```bash
# Basic conversion
go run examples/mcp-to-skill/main.go convert npx -y @modelcontextprotocol/server-fetch

# HTTP server
go run examples/mcp-to-skill/main.go convert-http http://localhost:38476/sse

# With LLM enhancement
OPENAI_API_KEY=sk-xxx go run examples/mcp-to-skill/main.go convert-llm npx -y @modelcontextprotocol/server-fetch

# With Ollama
OPENAI_API_KEY=dummy OPENAI_BASE_URL=http://localhost:11434/v1 \
  go run examples/mcp-to-skill/main.go convert-llm npx -y @modelcontextprotocol/server-fetch

# Discover capabilities
go run examples/mcp-to-skill/main.go discover npx -y @modelcontextprotocol/server-fetch
```

### Output Structure

Generated skills include:

```
my-mcp-skill/
├── SKILL.md              # Main skill file (LLM-enhanced or raw)
└── references/           # Detailed MCP capability documentation
    ├── tools.md          # Tool schemas and descriptions
    ├── resources.md      # Resource URIs and metadata
    └── prompts.md        # Prompt templates and arguments
```

### Comparison: LLM vs Raw Conversion

| Feature | Raw Conversion | LLM-Enhanced |
|---------|---------------|--------------|
| Speed | Fast (~1s) | Slower (~5-10s) |
| Description | Original MCP text | User-friendly summary |
| Examples | None | Practical usage examples |
| Organization | Flat list | Logical grouping |
| Cost | Free | LLM API cost |

## Usage Patterns

### Pattern 1: Integration (Inject Existing Client)
Use this if you already have an initialized `openai.Client`.

```go
myClient := openai.NewClient(option.WithAPIKey("..."))
cli := client.NewClient(nil, client.WithOpenAIClient(myClient))

ctx := context.Background()
cli.LoadSkills(ctx)
resp, _ := cli.Chat(ctx, "Help me with my code")
```

`Chat()` discloses a tier-1 skill catalog to the model and exposes a dedicated `activate_skill` tool. When the model decides a skill is relevant, it activates that skill on demand and receives structured skill content plus bundled resource paths.

If you pass `client.WithSessionID("...")`, activated skills are also replayed into later turns for that session so their instructions survive history truncation and duplicate activation attempts can be deduplicated.

### Pattern 2: Headless (Skill Management Only)
Use this if you want to manage skills but handle the LLM chat loop yourself.

```go
cli := client.NewClient(nil, client.WithSkillPaths("./skills"))
cli.LoadSkills(ctx)

// 1. Inspect the tier-1 catalog
catalog := cli.SkillCatalog()

// 2. Build a model-visible catalog prompt if you run your own chat loop
skills, _ := cli.Resolve(ctx, "Commit changes")
prompt := cli.BuildSystemPrompt(skills)

// 3. Activate the full skill only when needed
activated, _ := cli.ActivateSkill(ctx, "commit")

// 4. Execute a bundled script if the activated skill references one
result, _ := cli.ExecuteInteractive(ctx, "commit", "analyze")

_ = catalog
_ = prompt
_ = activated
```

You can also inspect non-fatal discovery warnings and trust skips:

```go
diagnostics := cli.SkillDiagnostics()
for _, d := range diagnostics {
    fmt.Println(d.Code, d.Message)
}
```

### Pattern 3: Standalone (Full Managed Agent)
Quickest way to build a skill-powered CLI.

```go
cli := client.NewClient(&client.Config{
    APIKey:     os.Getenv("OPENAI_API_KEY"),
    SkillPaths: []string{"./skills"},
})
cli.LoadSkills(ctx)
resp, _ := cli.Chat(ctx, "/my-skill arg1")
```

## Skill Format

Skills follow the Agent Skills format:

```
my-skill/
├── SKILL.md           # Required: main instructions with YAML frontmatter
├── scripts/           # Optional: executable scripts
├── references/        # Optional: documentation loaded on demand
└── assets/            # Optional: templates and static resources
```

### SKILL.md Format

```markdown
---
name: my-skill
description: What this skill does and when to use it
---

Your skill instructions here...
```

### Official Frontmatter Fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Required. Must match the parent directory name and follow the Agent Skills naming rules |
| `description` | string | Required. Describes what the skill does and when to use it |
| `license` | string | Optional license note |
| `compatibility` | string | Optional environment requirements |
| `metadata` | map[string]string | Optional implementation-specific metadata |
| `allowed-tools` | string | Optional space-delimited list of pre-approved tools |

### Repository Extensions

`skills-go` also supports extra frontmatter fields for runtime behavior, including `argument-hint`, `disable-model-invocation`, `user-invocable`, `model`, `context`, `agent`, and `hooks`. These are not part of the base Agent Skills specification.

## Progressive Disclosure

Discovery defaults to metadata-only loading. Full instructions and resources are loaded only when needed:

```go
loader := skill.NewLoader(skill.WithPaths(".agents/skills"))

// Level 1: Metadata only
s, _ := loader.LoadWithLevel(ctx, path, skill.LoadLevelMetadata)

// Level 2: Full SKILL.md content
s, _ = loader.LoadWithLevel(ctx, path, skill.LoadLevelContent)

// Level 3: Full (including scanning scripts/resources)
loader.EnsureLoaded(ctx, s, skill.LoadLevelFull)
```

From the higher-level client API, discovery stays metadata-only as well:

```go
cli.LoadSkills(ctx)

// Metadata only.
meta, _ := cli.GetSkill("my-skill")

// Upgrade on demand when you need instructions or resources.
full, _ := cli.GetSkillWithLevel(ctx, "my-skill", skill.LoadLevelContent)
scripts, _ := cli.GetSkillWithLevel(ctx, "my-skill", skill.LoadLevelFull)
```

`Chat()` uses the same model under the hood:

- The model sees a catalog containing `name`, `description`, and `location`
- The model can call `activate_skill(name)` to load the full instructions
- Activated skill payloads include the absolute skill directory plus bundled resource paths without eagerly reading those files

Headless integrations can use the same pieces directly:

```go
catalog := cli.SkillCatalog()
activated, _ := cli.ActivateSkill(ctx, "my-skill")

fmt.Println(catalog[0].Location)
fmt.Println(activated.Wrapped)
```

Project-level trust gating is configurable:

```go
cli := client.NewClient(
    nil,
    client.WithProjectSkillsTrusted(false),
)
```

The lower-level hook is `client.WithSkillTrustPolicy(...)`, which lets you allow or deny discovered skills based on scope and path.

## Variable Substitution

| Variable | Description |
|----------|-------------|
| `$ARGUMENTS` | All arguments |
| `$ARGUMENTS[N]` | Nth argument (0-based) |
| `$N` | Shorthand for `$ARGUMENTS[N]` |
| `${CLAUDE_SESSION_ID}` | Current session ID |
| `${VAR}` | Environment variable |

## Command Injection

```markdown
---
name: pr-summary
description: Summarize pull request changes
---
## PR Context
- Diff: !`gh pr diff`
- Comments: !`gh pr view --comments`

Summarize this PR...
```

## License

MIT
