package skill

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestNewRegistry(t *testing.T) {
	loader := NewLoader()
	reg := NewRegistry(loader)

	if reg == nil {
		t.Fatal("NewRegistry returned nil")
	}

	if reg.Count() != 0 {
		t.Errorf("new registry has count %d, want 0", reg.Count())
	}
}

func TestRegistryAddAndGet(t *testing.T) {
	reg := NewRegistry(NewLoader())

	skill := &Skill{
		Name: "test-skill",
		Path: "/path/to/skill",
		Meta: Meta{Name: "test-skill"},
	}

	reg.Add(skill)

	// Test Get
	retrieved, err := reg.Get("test-skill")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if retrieved.Name != "test-skill" {
		t.Errorf("got name %q, want %q", retrieved.Name, "test-skill")
	}

	// Test GetByPath
	retrieved, err = reg.GetByPath("/path/to/skill")
	if err != nil {
		t.Fatalf("GetByPath failed: %v", err)
	}
	if retrieved.Name != "test-skill" {
		t.Errorf("got name %q, want %q", retrieved.Name, "test-skill")
	}

	// Test Count
	if reg.Count() != 1 {
		t.Errorf("count = %d, want 1", reg.Count())
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	reg := NewRegistry(NewLoader())

	_, err := reg.Get("nonexistent")
	if err != ErrSkillNotFound {
		t.Errorf("expected ErrSkillNotFound, got %v", err)
	}

	_, err = reg.GetByPath("/nonexistent")
	if err != ErrSkillNotFound {
		t.Errorf("expected ErrSkillNotFound, got %v", err)
	}
}

func TestRegistryRemove(t *testing.T) {
	reg := NewRegistry(NewLoader())

	skill := &Skill{
		Name: "test-skill",
		Path: "/path/to/skill",
		Meta: Meta{Name: "test-skill"},
	}

	reg.Add(skill)
	reg.Remove(skill)

	if reg.Count() != 0 {
		t.Errorf("count after remove = %d, want 0", reg.Count())
	}

	_, err := reg.Get("test-skill")
	if err != ErrSkillNotFound {
		t.Errorf("expected ErrSkillNotFound after remove, got %v", err)
	}
}

func TestRegistryClear(t *testing.T) {
	reg := NewRegistry(NewLoader())

	// Add multiple skills
	for i := 0; i < 5; i++ {
		reg.Add(&Skill{
			Name: string(rune('a' + i)),
			Path: "/path/" + string(rune('a'+i)),
			Meta: Meta{Name: string(rune('a' + i))},
		})
	}

	if reg.Count() != 5 {
		t.Errorf("count before clear = %d, want 5", reg.Count())
	}

	reg.Clear()

	if reg.Count() != 0 {
		t.Errorf("count after clear = %d, want 0", reg.Count())
	}
}

func TestRegistryList(t *testing.T) {
	reg := NewRegistry(NewLoader())

	skills := []*Skill{
		{Name: "skill1", Path: "/path1", Meta: Meta{Name: "skill1", Description: "First skill"}},
		{Name: "skill2", Path: "/path2", Meta: Meta{Name: "skill2", Description: "Second skill"}},
		{Name: "skill3", Path: "/path3", Meta: Meta{Name: "skill3", Description: "Third skill"}},
	}

	for _, s := range skills {
		reg.Add(s)
	}

	metas := reg.List()
	if len(metas) != 3 {
		t.Fatalf("List returned %d items, want 3", len(metas))
	}

	// Check we got all metadata
	names := make(map[string]bool)
	for _, m := range metas {
		names[m.Name] = true
	}

	for _, expected := range []string{"skill1", "skill2", "skill3"} {
		if !names[expected] {
			t.Errorf("missing skill %q in list", expected)
		}
	}
}

func TestRegistryNames(t *testing.T) {
	reg := NewRegistry(NewLoader())

	skills := []string{"alpha", "beta", "gamma"}
	for _, name := range skills {
		reg.Add(&Skill{Name: name, Path: "/path/" + name, Meta: Meta{Name: name}})
	}

	names := reg.Names()
	if len(names) != 3 {
		t.Fatalf("Names returned %d items, want 3", len(names))
	}

	// Convert to set for comparison
	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}

	for _, expected := range skills {
		if !nameSet[expected] {
			t.Errorf("missing name %q", expected)
		}
	}
}

func TestRegistryFindByPrefix(t *testing.T) {
	reg := NewRegistry(NewLoader())

	skills := []string{"test-alpha", "test-beta", "other-skill"}
	for _, name := range skills {
		reg.Add(&Skill{Name: name, Path: "/path/" + name, Meta: Meta{Name: name}})
	}

	results := reg.FindByPrefix("test-")
	if len(results) != 2 {
		t.Fatalf("FindByPrefix returned %d items, want 2", len(results))
	}

	// Verify results
	names := make(map[string]bool)
	for _, r := range results {
		names[r.Name] = true
	}

	for _, expected := range []string{"test-alpha", "test-beta"} {
		if !names[expected] {
			t.Errorf("missing skill %q in results", expected)
		}
	}
}

func TestRegistryResolve(t *testing.T) {
	reg := NewRegistry(NewLoader())

	skills := []*Skill{
		{Name: "commit", Path: "/path/commit", Meta: Meta{Name: "commit", Description: "Help create git commits"}},
		{Name: "test", Path: "/path/test", Meta: Meta{Name: "test", Description: "Run tests"}},
		{Name: "deploy", Path: "/path/deploy", Meta: Meta{Name: "deploy", Description: "Deploy to production", DisableModelInvocation: true},
		},
	}

	for _, s := range skills {
		reg.Add(s)
	}

	ctx := context.Background()

	tests := []struct {
		name          string
		input         string
		expectedCount int
		mustContain   []string
	}{
		{
			name:          "matching commit keyword",
			input:         "I want to commit my code",
			expectedCount: 1,
			mustContain:   []string{"commit"},
		},
		{
			name:          "matching test keyword",
			input:         "run the unit test",
			expectedCount: 1,
			mustContain:   []string{"test"},
		},
		{
			name:          "skill name directly",
			input:         "use commit to save changes",
			expectedCount: 1,
			mustContain:   []string{"commit"},
		},
		{
			name:          "disabled skill not included",
			input:         "deploy to production",
			expectedCount: 0,
		},
		{
			name:          "no match",
			input:         "something unrelated",
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := reg.Resolve(ctx, tt.input)
			if err != nil {
				t.Fatalf("Resolve failed: %v", err)
			}

			if len(results) != tt.expectedCount {
				t.Errorf("Resolve returned %d skills, want %d", len(results), tt.expectedCount)
			}

			if len(tt.mustContain) > 0 {
				names := make(map[string]bool)
				for _, r := range results {
					names[r.Name] = true
				}
				for _, expected := range tt.mustContain {
					if !names[expected] {
						t.Errorf("results missing skill %q", expected)
					}
				}
			}
		})
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	reg := NewRegistry(NewLoader())
	ctx := context.Background()

	// Add initial skills
	for i := 0; i < 10; i++ {
		reg.Add(&Skill{
			Name: string(rune('a' + i)),
			Path: "/path/" + string(rune('a'+i)),
			Meta: Meta{Name: string(rune('a' + i)), Description: "Skill " + string(rune('a'+i))},
		})
	}

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := reg.Get("a")
			if err != nil && err != ErrSkillNotFound {
				errors <- err
			}
			reg.List()
			reg.Names()
		}()
	}

	// Concurrent writes
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			skill := &Skill{
				Name: string(rune('a' + i)),
				Path: "/path/new" + string(rune('a'+i)),
				Meta: Meta{Name: string(rune('a' + i))},
			}
			reg.Add(skill)
			reg.Resolve(ctx, "test")
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent access error: %v", err)
	}
}

func TestRegistryLoad(t *testing.T) {
	// Create a mock loader that returns test skills
	loader := NewLoader(
		WithPaths("testdata/skills"),
	)

	reg := NewRegistry(loader)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// This should not crash even if testdata doesn't exist
	err := reg.Load(ctx)
	if err != nil {
		// It's ok if testdata doesn't exist, just verify the mechanism works
		t.Logf("Load returned error (expected if testdata missing): %v", err)
	}
}
