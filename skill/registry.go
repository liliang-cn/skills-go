package skill

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// Registry manages skill discovery and resolution
type Registry struct {
	loader   *Loader
	skills   map[string]*Skill      // by path
	byName   map[string]*Skill      // by name
	handlers map[string]HandlerFunc // handler functions by name
	mu       sync.RWMutex
}

// NewRegistry creates a new skill registry
func NewRegistry(loader *Loader) *Registry {
	return &Registry{
		loader:   loader,
		skills:   make(map[string]*Skill),
		byName:   make(map[string]*Skill),
		handlers: make(map[string]HandlerFunc),
	}
}

// Load loads all skills from configured paths
func (r *Registry) Load(ctx context.Context) error {
	skills, err := r.loader.LoadAll(ctx)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, s := range skills {
		r.skills[s.Path] = s
		r.byName[s.Name] = s
	}

	return nil
}

// Diagnostics returns discovery diagnostics from the underlying loader.
func (r *Registry) Diagnostics() []Diagnostic {
	if r.loader == nil {
		return nil
	}
	return r.loader.Diagnostics()
}

// Get retrieves a skill by name
func (r *Registry) Get(name string) (*Skill, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	s, ok := r.byName[name]
	if !ok {
		return nil, ErrSkillNotFound
	}
	return s, nil
}

// GetWithLevel retrieves a skill by name and ensures it is loaded to the requested level.
func (r *Registry) GetWithLevel(ctx context.Context, name string, level LoadLevel) (*Skill, error) {
	s, err := r.Get(name)
	if err != nil {
		return nil, err
	}

	if err := r.loader.EnsureLoaded(ctx, s, level); err != nil {
		return nil, err
	}

	return s, nil
}

// GetByPath retrieves a skill by path
func (r *Registry) GetByPath(path string) (*Skill, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	s, ok := r.skills[path]
	if !ok {
		return nil, ErrSkillNotFound
	}
	return s, nil
}

// Resolve finds relevant skills based on user input
func (r *Registry) Resolve(ctx context.Context, input string) ([]*Skill, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	input = strings.ToLower(input)
	inputWords := extractWords(input)

	var relevant []*Skill

	for _, s := range r.byName {
		// Skip if model cannot invoke
		if !s.IsModelInvocable() {
			continue
		}

		// Check if description matches
		if r.matches(input, inputWords, s) {
			relevant = append(relevant, s)
		}
	}

	return relevant, nil
}

// ResolveForModel finds model-invocable skills relevant to the current task.
// It uses name, description, when_to_use, and optional path constraints.
func (r *Registry) ResolveForModel(ctx context.Context, input string, touchedPaths []string) ([]*Skill, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	inputLower := strings.ToLower(strings.TrimSpace(input))
	inputWords := extractWords(inputLower)

	type scoredSkill struct {
		skill *Skill
		score int
	}
	scored := make([]scoredSkill, 0, len(r.byName))

	for _, s := range r.byName {
		if !s.IsModelInvocable() {
			continue
		}
		if !s.MatchesAnyPath(touchedPaths) {
			continue
		}

		score := scoreSkillMatch(inputLower, inputWords, s)
		if score <= 0 {
			continue
		}
		scored = append(scored, scoredSkill{skill: s, score: score})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].skill.Name < scored[j].skill.Name
		}
		return scored[i].score > scored[j].score
	})

	result := make([]*Skill, 0, len(scored))
	for _, item := range scored {
		result = append(result, item.skill)
	}
	return result, nil
}

// matches checks if a skill matches the input
func (r *Registry) matches(input string, inputWords []string, s *Skill) bool {
	desc := strings.ToLower(s.Meta.Description)
	whenToUse := strings.ToLower(s.Meta.WhenToUse)

	// Check for exact phrase matches
	for _, word := range inputWords {
		if len(word) < 4 {
			continue // Skip short words
		}
		if strings.Contains(desc, word) || strings.Contains(whenToUse, word) {
			return true
		}
	}

	// Check for skill name match
	if strings.Contains(input, s.Name) {
		return true
	}

	return false
}

func scoreSkillMatch(input string, inputWords []string, s *Skill) int {
	score := 0
	name := strings.ToLower(s.Name)
	desc := strings.ToLower(s.Meta.Description)
	whenToUse := strings.ToLower(s.Meta.WhenToUse)

	if strings.Contains(input, name) {
		score += 80
	}
	for _, word := range inputWords {
		if len(word) < 3 {
			continue
		}
		if strings.Contains(name, word) {
			score += 30
		}
		if strings.Contains(whenToUse, word) {
			score += 20
		}
		if strings.Contains(desc, word) {
			score += 10
		}
	}
	return score
}

// extractWords extracts words from input for matching
func extractWords(input string) []string {
	replacer := strings.NewReplacer("/", " ", "\\", " ", ".", " ", "-", " ", "_", " ", ":", " ")
	words := strings.Fields(replacer.Replace(input))
	var result []string

	for _, w := range words {
		// Remove punctuation
		w = strings.Trim(w, ".,!?;:\"'")
		if len(w) >= 4 {
			result = append(result, strings.ToLower(w))
		}
	}

	return result
}

// List returns all skill metadata
func (r *Registry) List() []Meta {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var metas []Meta
	for _, s := range r.byName {
		metas = append(metas, s.Meta)
	}
	return metas
}

// ListSkills returns all skills
func (r *Registry) ListSkills() []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()

	skills := make([]*Skill, 0, len(r.byName))
	for _, s := range r.byName {
		skills = append(skills, s)
	}
	return skills
}

// ListCollections returns grouped collections for skills that belong to a bundle/source.
func (r *Registry) ListCollections() []*Collection {
	r.mu.RLock()
	defer r.mu.RUnlock()

	byName := make(map[string]*Collection)
	for _, s := range r.byName {
		if s.Collection == "" {
			continue
		}
		col, ok := byName[s.Collection]
		if !ok {
			col = &Collection{
				Name:  s.Collection,
				Path:  s.CollectionPath,
				Scope: s.Scope,
			}
			byName[s.Collection] = col
		}
		col.Skills = append(col.Skills, s)
	}

	collections := make([]*Collection, 0, len(byName))
	for _, c := range byName {
		sort.Slice(c.Skills, func(i, j int) bool {
			return c.Skills[i].Name < c.Skills[j].Name
		})
		collections = append(collections, c)
	}
	sort.Slice(collections, func(i, j int) bool {
		return collections[i].Name < collections[j].Name
	})
	return collections
}

// Add adds a skill to the registry
func (r *Registry) Add(skill *Skill) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.skills[skill.Path] = skill
	r.byName[skill.Name] = skill
}

// RegisterFunction registers a Go function as a skill
func (r *Registry) RegisterFunction(name, description string, handler HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Create a synthetic skill for the handler
	skill := &Skill{
		Name:        name,
		Description: description,
		Meta: Meta{
			Name:        name,
			Description: description,
		},
	}

	r.byName[name] = skill
	r.handlers[name] = handler
}

// RegisterHandlerSkill registers a HandlerSkill
func (r *Registry) RegisterHandlerSkill(h *HandlerSkill) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.byName[h.Name] = &h.Skill
	r.handlers[h.Name] = h.Handler
}

// GetHandler returns the handler function for a skill, or nil if not a handler skill
func (r *Registry) GetHandler(name string) HandlerFunc {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.handlers[name]
}

// IsHandlerSkill returns true if the skill has a registered handler function
func (r *Registry) IsHandlerSkill(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, ok := r.handlers[name]
	return ok
}

// Remove removes a skill from the registry
func (r *Registry) Remove(skill *Skill) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.skills, skill.Path)
	delete(r.byName, skill.Name)
	delete(r.handlers, skill.Name)
}

// Clear clears all skills from the registry
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.skills = make(map[string]*Skill)
	r.byName = make(map[string]*Skill)
	r.handlers = make(map[string]HandlerFunc)
}

// Count returns the number of registered skills
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.byName)
}

// Names returns all skill names
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.byName))
	for name := range r.byName {
		names = append(names, name)
	}
	return names
}

// FindByPrefix finds skills whose name starts with the given prefix
func (r *Registry) FindByPrefix(prefix string) []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var results []*Skill
	for _, s := range r.byName {
		if strings.HasPrefix(s.Name, prefix) {
			results = append(results, s)
		}
	}
	return results
}
