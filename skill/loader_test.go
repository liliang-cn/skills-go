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
	gotPath, _ = filepath.EvalSymlinks(gotPath)
	wantPath, _ = filepath.EvalSymlinks(wantPath)
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
	gotDiagPath, _ := filepath.EvalSymlinks(diagnostics[0].Path)
	wantDiagPath, _ := filepath.EvalSymlinks(skillLocation(userSkillPath))
	if gotDiagPath != wantDiagPath {
		t.Fatalf("Diagnostic path = %q, want %q", diagnostics[0].Path, skillLocation(userSkillPath))
	}
	gotShadowedBy, _ := filepath.EvalSymlinks(diagnostics[0].ShadowedBy)
	wantShadowedBy, _ := filepath.EvalSymlinks(skillLocation(projectSkillPath))
	if gotShadowedBy != wantShadowedBy {
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
	gotDiagPath, _ := filepath.EvalSymlinks(diagnostics[0].Path)
	wantDiagPath, _ := filepath.EvalSymlinks(skillLocation(projectSkillPath))
	if gotDiagPath != wantDiagPath {
		t.Fatalf("Diagnostic path = %q, want %q", diagnostics[0].Path, skillLocation(projectSkillPath))
	}
}

func TestLoaderAssignsCollectionForNestedSkills(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()

	nestedSkillPath := filepath.Join(baseDir, "impeccable", "frontend-design")
	writeTestSkill(t, nestedSkillPath, `---
name: frontend-design
description: Nested collection skill.
---
`)

	standaloneSkillPath := filepath.Join(baseDir, "pptx")
	writeTestSkill(t, standaloneSkillPath, `---
name: pptx
description: Standalone skill.
---
`)

	loader := NewLoader(WithPaths(baseDir))
	loader.paths = []string{baseDir}
	skills, err := loader.LoadAll(ctx)
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("LoadAll returned %d skills, want 2", len(skills))
	}

	var nested, standalone *Skill
	for _, skill := range skills {
		switch skill.Name {
		case "frontend-design":
			nested = skill
		case "pptx":
			standalone = skill
		}
	}

	if nested == nil {
		t.Fatal("nested skill not found")
	}
	if nested.Collection != "impeccable" {
		t.Fatalf("Collection = %q, want %q", nested.Collection, "impeccable")
	}
	wantCollectionPath := filepath.Join(baseDir, "impeccable")
	if nested.CollectionPath != wantCollectionPath {
		t.Fatalf("CollectionPath = %q, want %q", nested.CollectionPath, wantCollectionPath)
	}

	if standalone == nil {
		t.Fatal("standalone skill not found")
	}
	if standalone.Collection != "" {
		t.Fatalf("standalone Collection = %q, want empty", standalone.Collection)
	}
	if standalone.CollectionPath != "" {
		t.Fatalf("standalone CollectionPath = %q, want empty", standalone.CollectionPath)
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
