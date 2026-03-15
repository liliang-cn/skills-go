package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPaths(t *testing.T) {
	paths := DefaultPaths()
	if len(paths) < 2 {
		t.Fatalf("DefaultPaths returned %d paths, want at least 2", len(paths))
	}
	if paths[0] != ".agents/skills" {
		t.Fatalf("first default path = %q, want %q", paths[0], ".agents/skills")
	}
}

func TestLoaderLoadAllUsesMetadataLevel(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()
	skillPath := filepath.Join(baseDir, "sample-skill")

	if err := os.MkdirAll(filepath.Join(skillPath, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte(`---
name: sample-skill
description: A sample skill for loader tests.
---

# Sample
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "scripts", "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	loader := NewLoader()
	loader.paths = []string{baseDir}
	skills, err := loader.LoadAll(ctx)
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("LoadAll returned %d skills, want 1", len(skills))
	}

	got := skills[0]
	if got.LoadLevel != LoadLevelMetadata {
		t.Fatalf("LoadLevel = %v, want %v", got.LoadLevel, LoadLevelMetadata)
	}
	if got.Content != "" {
		t.Fatalf("Content = %q, want empty for metadata-only load", got.Content)
	}
	if got.Raw != "" {
		t.Fatalf("Raw should be empty for metadata-only load")
	}
	if got.Resources != nil {
		t.Fatalf("Resources should be nil for metadata-only load")
	}
}

func TestLoaderEnsureLoadedUpgradesSkill(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()
	skillPath := filepath.Join(baseDir, "sample-skill")

	if err := os.MkdirAll(filepath.Join(skillPath, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte(`---
name: sample-skill
description: A sample skill for ensure-loaded tests.
---

Body content.
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "scripts", "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	loader := NewLoader()
	loader.paths = []string{baseDir}
	skill, err := loader.LoadMetadata(ctx, skillPath)
	if err != nil {
		t.Fatalf("LoadMetadata failed: %v", err)
	}

	if err := loader.EnsureLoaded(ctx, skill, LoadLevelFull); err != nil {
		t.Fatalf("EnsureLoaded failed: %v", err)
	}
	if skill.LoadLevel != LoadLevelFull {
		t.Fatalf("LoadLevel = %v, want %v", skill.LoadLevel, LoadLevelFull)
	}
	if skill.Content == "" {
		t.Fatal("Content should be populated after full load")
	}
	if skill.Resources == nil || len(skill.Resources.Scripts) != 1 {
		t.Fatal("Resources should be populated after full load")
	}
}

func TestLoaderRejectsInvalidSkills(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()

	validPath := filepath.Join(baseDir, "valid-skill")
	invalidPath := filepath.Join(baseDir, "invalid-skill")

	if err := os.MkdirAll(validPath, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.MkdirAll(invalidPath, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(validPath, "SKILL.md"), []byte(`---
name: valid-skill
description: A valid skill.
---
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(invalidPath, "SKILL.md"), []byte(`---
name: invalid-skill
---
`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	loader := NewLoader()
	loader.paths = []string{baseDir}
	skills, err := loader.LoadAll(ctx)
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("LoadAll returned %d skills, want 1 valid skill", len(skills))
	}
	if skills[0].Name != "valid-skill" {
		t.Fatalf("loaded skill = %q, want %q", skills[0].Name, "valid-skill")
	}
}

func TestLoaderPrefersProjectSkillsOverUserSkillsAndReportsCollision(t *testing.T) {
	ctx := context.Background()
	homeDir := t.TempDir()
	projectDir := t.TempDir()
	setLoaderTestEnv(t, homeDir, projectDir)

	projectSkillPath := filepath.Join(projectDir, ".agents", "skills", "shared-skill")
	userSkillPath := filepath.Join(homeDir, ".agents", "skills", "shared-skill")

	writeTestSkill(t, projectSkillPath, `---
name: shared-skill
description: Project skill wins.
---

Project content.
`)
	writeTestSkill(t, userSkillPath, `---
name: shared-skill
description: User skill loses.
---

User content.
`)

	loader := NewLoader()
	skills, err := loader.LoadAll(ctx)
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("LoadAll returned %d skills, want 1", len(skills))
	}

	got := skills[0]
	gotPath, err := filepath.Abs(got.Path)
	if err != nil {
		t.Fatalf("Abs failed: %v", err)
	}
	wantPath, err := filepath.Abs(projectSkillPath)
	if err != nil {
		t.Fatalf("Abs failed: %v", err)
	}
	if gotPath != wantPath {
		t.Fatalf("Path = %q, want %q", gotPath, wantPath)
	}
	if got.Meta.Description != "Project skill wins." {
		t.Fatalf("Description = %q, want project skill description", got.Meta.Description)
	}
	if got.Scope != SkillScopeProject {
		t.Fatalf("Scope = %q, want %q", got.Scope, SkillScopeProject)
	}

	diagnostics := loader.Diagnostics()
	if len(diagnostics) != 1 {
		t.Fatalf("Diagnostics returned %d items, want 1", len(diagnostics))
	}
	if diagnostics[0].Code != "skill_name_collision" {
		t.Fatalf("Diagnostic code = %q, want %q", diagnostics[0].Code, "skill_name_collision")
	}
	if diagnostics[0].Path != skillLocation(userSkillPath) {
		t.Fatalf("Diagnostic path = %q, want %q", diagnostics[0].Path, skillLocation(userSkillPath))
	}
	if diagnostics[0].ShadowedBy != skillLocation(projectSkillPath) {
		t.Fatalf("Diagnostic shadowed_by = %q, want %q", diagnostics[0].ShadowedBy, skillLocation(projectSkillPath))
	}
}

func TestLoaderTrustPolicySkipsUntrustedProjectSkills(t *testing.T) {
	ctx := context.Background()
	homeDir := t.TempDir()
	projectDir := t.TempDir()
	setLoaderTestEnv(t, homeDir, projectDir)

	projectSkillPath := filepath.Join(projectDir, ".agents", "skills", "untrusted-skill")
	writeTestSkill(t, projectSkillPath, `---
name: untrusted-skill
description: Skipped by trust policy.
---
`)

	loader := NewLoader(WithTrustPolicy(func(scope SkillScope, skillPath string) (bool, string) {
		if scope == SkillScopeProject {
			return false, "project directory is not trusted"
		}
		return true, ""
	}))

	skills, err := loader.LoadAll(ctx)
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("LoadAll returned %d skills, want 0", len(skills))
	}

	diagnostics := loader.Diagnostics()
	if len(diagnostics) != 1 {
		t.Fatalf("Diagnostics returned %d items, want 1", len(diagnostics))
	}
	if diagnostics[0].Code != "untrusted_project_skill" {
		t.Fatalf("Diagnostic code = %q, want %q", diagnostics[0].Code, "untrusted_project_skill")
	}
	if diagnostics[0].Path != skillLocation(projectSkillPath) {
		t.Fatalf("Diagnostic path = %q, want %q", diagnostics[0].Path, skillLocation(projectSkillPath))
	}
}

func writeTestSkill(t *testing.T, skillPath string, content string) {
	t.Helper()
	if err := os.MkdirAll(skillPath, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}

func setLoaderTestEnv(t *testing.T, homeDir, projectDir string) {
	t.Helper()
	t.Setenv("HOME", homeDir)

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevWD)
	})
}
