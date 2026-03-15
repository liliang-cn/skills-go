package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultPaths returns the default Agent Skills search paths.
func DefaultPaths() []string {
	homeDir, _ := os.UserHomeDir()

	paths := []string{".agents/skills"}
	if homeDir != "" {
		paths = append(paths, filepath.Join(homeDir, ".agents/skills"))
	}

	return paths
}

// Loader loads skills from filesystem
type Loader struct {
	paths       []string
	cache       *Cache
	trustPolicy TrustPolicy
	diagnostics []Diagnostic
	diagMu      sync.RWMutex
}

// LoaderOption configures a Loader
type LoaderOption func(*Loader)

// WithPaths adds search paths for skills
func WithPaths(paths ...string) LoaderOption {
	return func(l *Loader) {
		l.paths = append(l.paths, paths...)
	}
}

// WithCache sets the cache for the loader
func WithCache(cache *Cache) LoaderOption {
	return func(l *Loader) {
		l.cache = cache
	}
}

// WithTrustPolicy sets the policy used to allow or deny discovered skills.
func WithTrustPolicy(policy TrustPolicy) LoaderOption {
	return func(l *Loader) {
		l.trustPolicy = policy
	}
}

// NewLoader creates a new skill loader
func NewLoader(opts ...LoaderOption) *Loader {
	l := &Loader{
		paths: DefaultPaths(),
		cache: NewCache(),
	}

	for _, opt := range opts {
		opt(l)
	}

	return l
}

// Load loads a single skill from the specified path with full details
func (l *Loader) Load(ctx context.Context, skillPath string) (*Skill, error) {
	return l.LoadWithLevel(ctx, skillPath, LoadLevelFull)
}

// LoadMetadata loads only skill metadata from SKILL.md.
func (l *Loader) LoadMetadata(ctx context.Context, skillPath string) (*Skill, error) {
	return l.LoadWithLevel(ctx, skillPath, LoadLevelMetadata)
}

// LoadWithLevel loads a skill with a specific level of detail
func (l *Loader) LoadWithLevel(ctx context.Context, skillPath string, level LoadLevel) (*Skill, error) {
	// Check cache first
	if cached, ok := l.cache.Get(skillPath); ok {
		// If cached version has equal or higher level, return it
		if cached.LoadLevel >= level {
			return cached, nil
		}
		// Otherwise we need to reload or upgrade. For simplicity, we proceed to reload.
	}

	// Find SKILL.md
	skillFile := filepath.Join(skillPath, "SKILL.md")
	if _, err := os.Stat(skillFile); err != nil {
		// Maybe skillPath points directly to SKILL.md
		if filepath.Base(skillPath) == "SKILL.md" {
			skillFile = skillPath
			skillPath = filepath.Dir(skillPath)
		} else {
			return nil, ErrSkillNotFound
		}
	}

	// Read file
	content, err := os.ReadFile(skillFile)
	if err != nil {
		return nil, err
	}

	// Parse frontmatter and content
	meta, markdown, err := ParseFrontmatter(content)
	if err != nil {
		return nil, err
	}

	if err := ValidateMeta(meta, skillPath); err != nil {
		return nil, err
	}

	skill := &Skill{
		Meta:     *meta,
		Path:     skillPath,
		Name:     meta.Name,
		Scope:    classifySkillScope(skillPath),
		LoadedAt: time.Now(),
	}

	if level >= LoadLevelContent {
		skill.Content = markdown
		skill.Raw = string(content)
		skill.LoadLevel = LoadLevelContent
	} else {
		skill.LoadLevel = LoadLevelMetadata
	}

	// Load resources if requested
	if level >= LoadLevelFull {
		resources, err := l.loadResources(skillPath)
		if err != nil {
			return nil, err
		}
		skill.Resources = resources
		skill.LoadLevel = LoadLevelFull
	}

	// Cache it
	l.cache.Set(skillPath, skill)

	return skill, nil
}

// EnsureLoaded ensures the skill is loaded to the specified level
func (l *Loader) EnsureLoaded(ctx context.Context, skill *Skill, level LoadLevel) error {
	if effectiveLoadLevel(skill) >= level {
		return nil
	}

	if skill.Path == "" {
		return nil
	}

	loaded, err := l.LoadWithLevel(ctx, skill.Path, level)
	if err != nil {
		return err
	}

	*skill = *loaded
	l.cache.Set(skill.Path, skill)
	return nil
}

// LoadAll loads all discoverable skills
func (l *Loader) LoadAll(ctx context.Context) ([]*Skill, error) {
	l.resetDiagnostics()

	var skills []*Skill
	seen := make(map[string]bool)
	byName := make(map[string]int)

	for _, basePath := range l.paths {
		entries, err := os.ReadDir(basePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			skillPath := filepath.Join(basePath, entry.Name())
			if seen[skillPath] {
				continue
			}
			seen[skillPath] = true

			scope := classifySkillScope(skillPath)
			if !l.isTrusted(scope, skillPath) {
				continue
			}

			skill, err := l.LoadMetadata(ctx, skillPath)
			if err != nil {
				continue // Skip invalid skills
			}
			skill.Scope = scope

			if idx, exists := byName[skill.Name]; exists {
				existing := skills[idx]
				if shouldReplaceSkill(existing, skill) {
					l.addDiagnostic(Diagnostic{
						Severity:   DiagnosticSeverityWarning,
						Code:       "skill_name_collision",
						Message:    fmt.Sprintf("skill %q from %s overrides %s due to precedence", skill.Name, skillLocation(skillPath), skillLocation(existing.Path)),
						SkillName:  existing.Name,
						Path:       skillLocation(existing.Path),
						Scope:      existing.Scope,
						ShadowedBy: skillLocation(skillPath),
					})
					skills[idx] = skill
					continue
				}

				l.addDiagnostic(Diagnostic{
					Severity:   DiagnosticSeverityWarning,
					Code:       "skill_name_collision",
					Message:    fmt.Sprintf("skill %q from %s was shadowed by %s", skill.Name, skillLocation(skillPath), skillLocation(existing.Path)),
					SkillName:  skill.Name,
					Path:       skillLocation(skillPath),
					Scope:      skill.Scope,
					ShadowedBy: skillLocation(existing.Path),
				})
				continue
			}

			byName[skill.Name] = len(skills)
			skills = append(skills, skill)
		}
	}

	return skills, nil
}

// Discover recursively discovers skills from a starting path
func (l *Loader) Discover(ctx context.Context, startPath string) ([]*Skill, error) {
	l.resetDiagnostics()

	var skills []*Skill
	byName := make(map[string]int)

	err := filepath.Walk(startPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if info.IsDir() {
			// Check if contains SKILL.md
			skillFile := filepath.Join(path, "SKILL.md")
			if _, err := os.Stat(skillFile); err == nil {
				scope := classifySkillScope(path)
				if !l.isTrusted(scope, path) {
					return filepath.SkipDir
				}

				skill, err := l.LoadMetadata(ctx, path)
				if err == nil {
					skill.Scope = scope
					if idx, exists := byName[skill.Name]; exists {
						existing := skills[idx]
						if shouldReplaceSkill(existing, skill) {
							l.addDiagnostic(Diagnostic{
								Severity:   DiagnosticSeverityWarning,
								Code:       "skill_name_collision",
								Message:    fmt.Sprintf("skill %q from %s overrides %s due to precedence", skill.Name, skillLocation(path), skillLocation(existing.Path)),
								SkillName:  existing.Name,
								Path:       skillLocation(existing.Path),
								Scope:      existing.Scope,
								ShadowedBy: skillLocation(path),
							})
							skills[idx] = skill
						} else {
							l.addDiagnostic(Diagnostic{
								Severity:   DiagnosticSeverityWarning,
								Code:       "skill_name_collision",
								Message:    fmt.Sprintf("skill %q from %s was shadowed by %s", skill.Name, skillLocation(path), skillLocation(existing.Path)),
								SkillName:  skill.Name,
								Path:       skillLocation(path),
								Scope:      skill.Scope,
								ShadowedBy: skillLocation(existing.Path),
							})
						}
					} else {
						byName[skill.Name] = len(skills)
						skills = append(skills, skill)
					}
				}
				return filepath.SkipDir
			}
		}

		return nil
	})

	return skills, err
}

// loadResources loads supporting files from skill directory
func (l *Loader) loadResources(skillPath string) (*Resources, error) {
	r := &Resources{}

	// Load scripts
	if scriptsDir := filepath.Join(skillPath, "scripts"); dirExists(scriptsDir) {
		entries, _ := os.ReadDir(scriptsDir)
		for _, e := range entries {
			if !e.IsDir() {
				r.Scripts = append(r.Scripts, Script{
					Name:     strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())),
					Path:     filepath.Join(scriptsDir, e.Name()),
					Language: strings.TrimPrefix(filepath.Ext(e.Name()), "."),
				})
			}
		}
	}

	// Load references
	if refsDir := filepath.Join(skillPath, "references"); dirExists(refsDir) {
		entries, _ := os.ReadDir(refsDir)
		for _, e := range entries {
			if !e.IsDir() {
				r.References = append(r.References, Reference{
					Name: strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())),
					Path: filepath.Join(refsDir, e.Name()),
				})
			}
		}
	}

	// Load assets
	if assetsDir := filepath.Join(skillPath, "assets"); dirExists(assetsDir) {
		entries, _ := os.ReadDir(assetsDir)
		for _, e := range entries {
			if !e.IsDir() {
				r.Assets = append(r.Assets, Asset{
					Name: e.Name(),
					Path: filepath.Join(assetsDir, e.Name()),
					Type: strings.TrimPrefix(filepath.Ext(e.Name()), "."),
				})
			}
		}
	}

	// Load root directory templates
	entries, _ := os.ReadDir(skillPath)
	for _, e := range entries {
		if !e.IsDir() && e.Name() != "SKILL.md" {
			ext := filepath.Ext(e.Name())
			if ext == ".md" || ext == ".txt" || ext == ".tmpl" {
				r.Templates = append(r.Templates, Template{
					Name: strings.TrimSuffix(e.Name(), ext),
					Path: filepath.Join(skillPath, e.Name()),
				})
			}
		}
	}

	return r, nil
}

// dirExists checks if a directory exists
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// Diagnostics returns non-fatal discovery diagnostics from the last discovery run.
func (l *Loader) Diagnostics() []Diagnostic {
	l.diagMu.RLock()
	defer l.diagMu.RUnlock()

	diagnostics := make([]Diagnostic, len(l.diagnostics))
	copy(diagnostics, l.diagnostics)
	return diagnostics
}

func (l *Loader) resetDiagnostics() {
	l.diagMu.Lock()
	defer l.diagMu.Unlock()

	l.diagnostics = nil
}

func (l *Loader) addDiagnostic(d Diagnostic) {
	l.diagMu.Lock()
	defer l.diagMu.Unlock()

	l.diagnostics = append(l.diagnostics, d)
}

func (l *Loader) isTrusted(scope SkillScope, skillPath string) bool {
	if l.trustPolicy == nil {
		return true
	}
	allowed, reason := l.trustPolicy(scope, skillPath)
	if allowed {
		return true
	}

	if reason == "" {
		reason = "blocked by trust policy"
	}
	l.addDiagnostic(Diagnostic{
		Severity:  DiagnosticSeverityWarning,
		Code:      "untrusted_project_skill",
		Message:   fmt.Sprintf("skill at %s was skipped: %s", skillLocation(skillPath), reason),
		Path:      skillLocation(skillPath),
		Scope:     scope,
		SkillName: filepath.Base(skillPath),
	})
	return false
}

func classifySkillScope(skillPath string) SkillScope {
	absSkillPath, err := filepath.Abs(skillPath)
	if err != nil {
		absSkillPath = skillPath
	}

	if cwd, err := os.Getwd(); err == nil {
		if pathWithin(absSkillPath, cwd) {
			return SkillScopeProject
		}
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if pathWithin(absSkillPath, home) {
			return SkillScopeUser
		}
	}

	return SkillScopeCustom
}

func pathWithin(path, root string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func shouldReplaceSkill(existing, candidate *Skill) bool {
	if existing == nil {
		return true
	}
	if existing.Scope == SkillScopeUser && candidate.Scope == SkillScopeProject {
		return true
	}
	return false
}

func skillLocation(skillPath string) string {
	location, err := filepath.Abs(filepath.Join(skillPath, "SKILL.md"))
	if err != nil {
		return filepath.Join(skillPath, "SKILL.md")
	}
	return location
}
