package skill

import (
	"context"
	"testing"
)

// realisticRegistry mirrors an actual installed skill set, using the real
// descriptions — including the one whose trailing "strict pre-flight check" is
// what made the old substring scorer match a weather question.
func realisticRegistry() *Registry {
	reg := NewRegistry(NewLoader())
	add := func(name, desc, when string) {
		reg.Add(&Skill{
			Name: name, Description: desc, WhenToUse: when,
			Meta: Meta{Name: name, Description: desc, WhenToUse: when},
		})
	}
	add("design-taste-frontend",
		"Anti-slop frontend skill for landing pages, portfolios, and redesigns. The agent reads the brief, infers the right design direction, and ships interfaces that do not look templated. Real design systems when applicable, audit-first on redesigns, strict pre-flight check.", "")
	add("brandkit",
		"Premium brand-kit image generation skill for creating high-end brand-guidelines boards, logo systems, identity decks, and visual-world presentations.", "")
	add("cortexdb",
		"Use CortexDB for vector search, hybrid search, knowledge graphs, and RAG.",
		"Use when working with embeddings, similarity search, semantic search, AI knowledge bases.")
	add("gpt-taste",
		"Elite UX/UI and Advanced GSAP Motion Engineer. Enforces Python-driven true randomization for layout variance, strict AIDA page structure.", "")
	add("stitch-design-taste",
		"Semantic Design System Skill for Google Stitch. Generates agent-friendly DESIGN.md files that enforce premium, anti-generic UI standards.", "")
	add("golang-pro",
		"Implements concurrent Go patterns using goroutines and channels, designs microservices with gRPC or REST.",
		"Use when building Go applications requiring concurrent programming or microservices.")
	return reg
}

func topScore(t *testing.T, reg *Registry, query string) (string, float64) {
	t.Helper()
	res, err := reg.ResolveForModelScored(context.Background(), query, ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveForModelScored: %v", err)
	}
	if len(res) == 0 {
		return "", 0
	}
	return res[0].Skill.Name, res[0].Score
}

// The gap DefaultMinScore sits in. If this test starts failing, the constant
// needs recalibrating — it is not an arbitrary number.
func TestRelevanceSeparatesRealMatchesFromIncidentalWords(t *testing.T) {
	reg := realisticRegistry()

	irrelevant := []string{
		"Check the weather in Chicago. If it's sunny, remind me to hang the laundry outside; otherwise remind me to use the dryer.",
		"Check the weather in Denver — if it's sunny, remind me to run at 6pm; if not, remind me to hit the gym.",
		"My test scores are 88, 92, 79, and 85. Calculate the average, and if it's below 85, remind me to study harder.",
	}
	for _, q := range irrelevant {
		name, score := topScore(t, reg, q)
		if score >= DefaultMinScore {
			t.Errorf("question unrelated to every skill matched %s at %.3f (floor %.2f): %.50s",
				name, score, DefaultMinScore, q)
		}
	}

	relevant := map[string]string{
		"I need a landing page for my startup, make the design look premium": "design-taste-frontend",
		"build me a brand kit with a logo system":                            "brandkit",
		"write a concurrent Go service with goroutines":                      "golang-pro",
	}
	for q, want := range relevant {
		name, score := topScore(t, reg, q)
		if name != want {
			t.Errorf("expected %s for %q, got %s (%.3f)", want, q, name, score)
		}
		if score < DefaultMinScore {
			t.Errorf("genuine match %s scored %.3f, below the floor %.2f: %q", name, score, DefaultMinScore, q)
		}
	}
}

// The specific regression: one shared word, in a long question about something
// else entirely, used to be enough.
func TestIncidentalWordDoesNotMakeASkillRelevant(t *testing.T) {
	reg := realisticRegistry()

	res, err := reg.ResolveForModelScored(context.Background(),
		"Check the weather in Chicago. If it's sunny, remind me to hang the laundry outside; otherwise remind me to use the dryer.",
		ResolveOptions{MinScore: DefaultMinScore})
	if err != nil {
		t.Fatalf("ResolveForModelScored: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("expected no skill for a weather question, got %+v", res[0].Skill.Name)
	}
}

// Word boundaries: a term must be a word in the skill's text, not a fragment
// buried inside another word.
func TestMatchingIsAtWordBoundaries(t *testing.T) {
	reg := NewRegistry(NewLoader())
	reg.Add(&Skill{
		Name: "starter", Description: "Start a category of things.",
		Meta: Meta{Name: "starter", Description: "Start a category of things."},
	})

	for _, fragment := range []string{"art", "cat", "ego"} {
		res, err := reg.ResolveForModelScored(context.Background(), fragment+" please", ResolveOptions{})
		if err != nil {
			t.Fatalf("ResolveForModelScored: %v", err)
		}
		if len(res) != 0 {
			t.Errorf("%q matched as a substring (score %.3f); it is not a word in the skill text",
				fragment, res[0].Score)
		}
	}

	// The whole words are still found.
	if name, score := topScore(t, reg, "start a category"); name != "starter" || score <= 0 {
		t.Errorf("expected whole words to match, got %q (%.3f)", name, score)
	}
}

// Naming a skill is not a guess, and outranks everything.
func TestNamingASkillScoresTop(t *testing.T) {
	reg := realisticRegistry()
	name, score := topScore(t, reg, "use the brandkit skill")
	if name != "brandkit" {
		t.Fatalf("expected brandkit, got %q", name)
	}
	if score != scoreNamedOutright {
		t.Errorf("expected a named skill to score %.1f, got %.3f", scoreNamedOutright, score)
	}
}

// Chinese has no spaces, so the substring scorer could only ever match a whole
// phrase and in practice matched nothing at all. Bigrams make it work.
func TestChineseInputMatchesChineseSkillText(t *testing.T) {
	reg := NewRegistry(NewLoader())
	reg.Add(&Skill{
		Name: "frontend-cn", Description: "前端落地页设计与实现",
		Meta: Meta{Name: "frontend-cn", Description: "前端落地页设计与实现"},
	})
	reg.Add(&Skill{
		Name: "finance-cn", Description: "股票投资与财报分析",
		Meta: Meta{Name: "finance-cn", Description: "股票投资与财报分析"},
	})

	name, score := topScore(t, reg, "帮我做一个前端落地页设计")
	if name != "frontend-cn" {
		t.Fatalf("expected frontend-cn for a Chinese frontend request, got %q (%.3f)", name, score)
	}
	if score <= 0 {
		t.Errorf("expected a positive score, got %.3f", score)
	}
}

// A skill's own path globs are its author saying when it applies.
func TestPathMatchCarriesTheScore(t *testing.T) {
	reg := NewRegistry(NewLoader())
	reg.Add(&Skill{
		Name: "docs-review", Description: "Review documentation",
		WhenToUse: "Use when editing markdown docs or README files.",
		Paths:     []string{"docs/*.md"},
		Meta: Meta{
			Name: "docs-review", Description: "Review documentation",
			WhenToUse: "Use when editing markdown docs or README files.",
			Paths:     []string{"docs/*.md"},
		},
	})

	res, err := reg.ResolveForModelScored(context.Background(), "tidy this up",
		ResolveOptions{TouchedPaths: []string{"docs/intro.md"}, MinScore: DefaultMinScore})
	if err != nil {
		t.Fatalf("ResolveForModelScored: %v", err)
	}
	if len(res) == 0 {
		t.Fatal("a declared path glob matching a file in play must select the skill")
	}
	if res[0].Score < scorePathMatch {
		t.Errorf("expected at least %.1f for a path match, got %.3f", scorePathMatch, res[0].Score)
	}
}

// MinScore and Limit both take effect, and the old signature still returns the
// unfiltered ranked list.
func TestResolveOptionsFloorAndLimit(t *testing.T) {
	reg := realisticRegistry()
	q := "I need a landing page for my startup, make the design look premium"

	all, err := reg.ResolveForModelScored(context.Background(), q, ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveForModelScored: %v", err)
	}
	floored, err := reg.ResolveForModelScored(context.Background(), q, ResolveOptions{MinScore: DefaultMinScore})
	if err != nil {
		t.Fatalf("ResolveForModelScored: %v", err)
	}
	if len(floored) >= len(all) {
		t.Errorf("the floor dropped nothing: %d vs %d", len(floored), len(all))
	}

	limited, err := reg.ResolveForModelScored(context.Background(), q, ResolveOptions{Limit: 1})
	if err != nil {
		t.Fatalf("ResolveForModelScored: %v", err)
	}
	if len(limited) != 1 {
		t.Errorf("expected Limit=1 to return one result, got %d", len(limited))
	}

	// Old signature: same ranking, no floor.
	legacy, err := reg.ResolveForModel(context.Background(), q, nil)
	if err != nil {
		t.Fatalf("ResolveForModel: %v", err)
	}
	if len(legacy) != len(all) {
		t.Errorf("ResolveForModel should be unfiltered: %d vs %d", len(legacy), len(all))
	}
	if len(legacy) > 0 && legacy[0].Name != all[0].Skill.Name {
		t.Errorf("ranking diverged: %q vs %q", legacy[0].Name, all[0].Skill.Name)
	}
}
