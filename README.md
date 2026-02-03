# skills-go

A Go library for loading and using Claude Skills with OpenAI API (v3.17.0).

## Features

- **Skill Loading**: Load skills from any location (personal, project, plugin)
- **Progressive Disclosure**: Metadata (Level 1) → Content (Level 2) → Full Resources (Level 3)
- **Interactive Execution**: Built-in PTY and Pipe support via `pipeit` for interactive scripts
- **Variable Substitution**: `$ARGUMENTS`, `$N`, `${CLAUDE_SESSION_ID}`
- **Command Injection**: Dynamic context with `!command` syntax
- **Fork Mode**: Run skills in isolated subagent contexts
- **Extensible**: Inject your own OpenAI client or use as a standalone skill manager
- **Standard Compliant**: Fully adheres to Claude Skills specifications

## Installation

```bash
go get github.com/liliang-cn/skills-go
```

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

### Pattern 2: Headless (Skill Management Only)
Use this if you want to manage skills but handle the LLM chat loop yourself.

```go
cli := client.NewClient(nil, client.WithSkillPaths("./skills"))
cli.LoadSkills(ctx)

// 1. Resolve skills for a query
skills, _ := cli.Resolve(ctx, "Commit changes")

// 2. Build system prompt for LLM
prompt := cli.BuildSystemPrompt(skills)

// 3. Execute matched skill script
result, _ := cli.ExecuteInteractive(ctx, "commit", "analyze")
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

Skills follow the Claude Skills standard:

```
my-skill/
├── SKILL.md           # Required: main instructions with YAML frontmatter
├── template.md        # Optional: template files
├── examples/          # Optional: example outputs
└── scripts/           # Optional: executable scripts
```

### SKILL.md Format

```markdown
---
name: my-skill
description: What this skill does and when to use it
disable-model-invocation: true
context: fork
agent: Explore
---

Your skill instructions here...

Arguments: $ARGUMENTS
Session: ${CLAUDE_SESSION_ID}
```

### Frontmatter Fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Skill name (defaults to directory name) |
| `description` | string | What the skill does and when to use it |
| `argument-hint` | string | Hint for expected arguments |
| `disable-model-invocation` | bool | Prevent automatic invocation |
| `user-invocable` | bool | Allow user invocation (default: true) |
| `allowed-tools` | []string | Tools Claude can use without permission |
| `model` | string | Model to use for this skill |
| `context` | string | Set to "fork" for isolated execution |
| `agent` | string | Agent type when forking |

## Progressive Disclosure

To save memory and tokens, you can load skills at different levels:

```go
loader := skill.NewLoader(skill.WithPaths("./skills"))

// Level 1: Metadata only
s, _ := loader.LoadWithLevel(ctx, path, skill.LoadLevelMetadata)

// Level 3: Full (including scanning scripts/resources)
loader.EnsureLoaded(ctx, s, skill.LoadLevelFull)
```

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