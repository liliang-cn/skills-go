package skill

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// Scoring which skills are relevant to what the user asked for.
//
// The previous scorer asked, for every input word of four characters or more,
// whether that word appeared as a SUBSTRING of a skill's name, when_to_use or
// description, and added a fixed number of points for each hit. Two things went
// wrong with that, and both of them cost real work downstream.
//
// Substrings matched inside other words: "art" hit "start", "cat" hit
// "category". And because the points were a fixed unbounded sum, a single
// common word carried as much weight as a genuine topical match — the question
// "Check the weather in Chicago…" scored a frontend design skill, because that
// skill's description happens to end with "strict pre-flight check". Any score
// above zero counted as relevant, so one incidental word was enough.
//
// Word boundaries alone would not have caught that example: "check" really is a
// whole word in both texts. What was missing is a sense of proportion. So the
// score here is a RATIO rather than a sum — how much of what the user asked for
// does this skill actually account for — and each matched term is weighted by
// how much it distinguishes this skill from the others in the registry.
//
// That weighting is inverse document frequency over the registry's own skills:
// a term that most skills mention tells you nothing about which one to pick, a
// term only one skill mentions tells you a great deal. It needs no list of
// stopwords to maintain, and it adapts to whatever the installed skills happen
// to talk about — "design" is uninformative in a registry of design skills and
// informative in a registry of one.

// DefaultMinScore is the relevance below which a match is not worth acting on.
//
// Calibrated against a real installed skill set (see TestRelevanceSeparates…):
// questions with nothing to do with any skill score 0.012-0.044 on incidental
// words, while a skill whose stated purpose is the task scores 0.13-0.29, and
// naming a skill outright scores 1.0. 0.10 sits in the gap with roughly a
// factor of two of clearance on each side.
//
// It is a floor, not a ranking: everything above it is still ordered by score.
// A caller that wants the unfiltered list can pass 0.
const DefaultMinScore = 0.10

// Relevance weights per field. A skill's name is the strongest statement of
// what it is, when_to_use is the author's own description of when it applies,
// and the description is prose that may wander.
const (
	fieldWeightName      = 3.0
	fieldWeightWhenToUse = 2.0
	fieldWeightDesc      = 1.0
)

// Confidence floors for the two signals that are not guesses at all: the user
// naming the skill, and the skill's own path globs matching a file in play.
const (
	scoreNamedOutright = 1.0
	scorePathMatch     = 0.7
)

// minTokenRunes is the shortest token worth indexing. Two runes admits "go",
// "ui", "db"; one rune is noise in every language except CJK, which is
// tokenised as bigrams below and so arrives at two runes anyway.
const minTokenRunes = 2

// ScoredSkill pairs a resolved skill with how relevant it is, in [0,1].
type ScoredSkill struct {
	Skill *Skill  `json:"skill"`
	Score float64 `json:"score"`
}

// ResolveOptions controls skill resolution.
type ResolveOptions struct {
	// TouchedPaths are the files in play, used both to filter path-conditional
	// skills and as a strong positive signal when one matches.
	TouchedPaths []string
	// MinScore drops matches below this relevance. Zero returns everything
	// with any overlap at all, which is the old behaviour.
	MinScore float64
	// Limit caps the number of results. Zero means no cap.
	Limit int
}

// tokenize splits text into comparable terms at word boundaries.
//
// Runs of letters and digits become tokens. CJK has no spaces, so each run of
// CJK is emitted as overlapping bigrams — the standard trick, and the reason
// this matcher works at all on Chinese input, which the substring scorer could
// only match by whole phrase and therefore essentially never did.
func tokenize(text string) []string {
	var (
		out   []string
		latin []rune
		cjk   []rune
	)
	flushLatin := func() {
		if len(latin) >= minTokenRunes {
			out = append(out, string(latin))
		}
		latin = latin[:0]
	}
	flushCJK := func() {
		switch {
		case len(cjk) == 1:
			out = append(out, string(cjk))
		default:
			for i := 0; i+1 < len(cjk); i++ {
				out = append(out, string(cjk[i:i+2]))
			}
		}
		cjk = cjk[:0]
	}

	for _, r := range strings.ToLower(text) {
		switch {
		case isCJK(r):
			flushLatin()
			cjk = append(cjk, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushCJK()
			latin = append(latin, r)
		default:
			flushLatin()
			flushCJK()
		}
	}
	flushLatin()
	flushCJK()
	return out
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}

// skillTerms is one skill's searchable text, split by field weight.
type skillTerms struct {
	byTerm map[string]float64 // term -> best field weight
}

func newSkillTerms(s *Skill) skillTerms {
	st := skillTerms{byTerm: make(map[string]float64)}
	add := func(text string, weight float64) {
		for _, term := range tokenize(text) {
			if st.byTerm[term] < weight {
				st.byTerm[term] = weight
			}
		}
	}
	// The struct fields and the frontmatter can each be the populated one
	// depending on how the skill was loaded; index both.
	add(s.Description, fieldWeightDesc)
	add(s.Meta.Description, fieldWeightDesc)
	add(s.WhenToUse, fieldWeightWhenToUse)
	add(s.Meta.WhenToUse, fieldWeightWhenToUse)
	add(s.Name, fieldWeightName)
	add(s.Meta.Name, fieldWeightName)
	return st
}

// corpus holds the per-registry statistics the scorer needs.
type corpus struct {
	terms map[*Skill]skillTerms
	idf   map[string]float64
	df    map[string]int
}

// buildCorpus indexes the candidate skills and computes each term's inverse
// document frequency across them.
func buildCorpus(skills []*Skill) *corpus {
	c := &corpus{
		terms: make(map[*Skill]skillTerms, len(skills)),
		idf:   make(map[string]float64),
		df:    make(map[string]int),
	}
	df := c.df
	for _, s := range skills {
		st := newSkillTerms(s)
		c.terms[s] = st
		for term := range st.byTerm {
			df[term]++
		}
	}
	n := float64(len(skills))
	for term, count := range df {
		// Smoothed IDF, always positive: a term every skill shares still counts
		// for a little, it just cannot carry a match on its own.
		c.idf[term] = math.Log(1 + n/(1+float64(count)))
	}
	return c
}

// idfOf returns the weight of a query term. A term no skill mentions is scored
// as maximally distinguishing, so that a query full of unknown words is not
// quietly treated as if it matched everything.
func (c *corpus) idfOf(term string) float64 {
	if v, ok := c.idf[term]; ok {
		return v
	}
	return math.Log(1 + float64(len(c.terms)))
}

// commonTermDocRatio is the share of skills a term may appear in before it
// stops counting as evidence FOR any one of them.
//
// "use", "file", "agent", "code" turn up in most skill descriptions. In a long
// question they are harmless — they are a small part of the total. In a short
// one they are most of it, and a skill could be selected almost entirely on the
// word "use". Such a term still counts toward the total (the user did say it,
// and nothing explained it), it just cannot be what selects a skill.
//
// This is the same idea as a stopword list, except derived from the skills that
// are actually installed rather than written down in advance: in a registry of
// design skills "design" is uninformative, in a registry of one it is decisive.
const commonTermDocRatio = 0.5

// minSkillsForCommonTerms is the corpus size below which the "most skills say
// this" statistic means nothing and is not applied.
const minSkillsForCommonTerms = 4

// distinguishes reports whether a term is specific enough to select a skill.
func (c *corpus) distinguishes(term string) bool {
	if len(c.terms) < minSkillsForCommonTerms {
		return true
	}
	return float64(c.df[term]) <= commonTermDocRatio*float64(len(c.terms))
}

// score returns how much of the query this skill accounts for, in [0,1].
func (c *corpus) score(s *Skill, query string, queryTerms []string, touchedPaths []string) float64 {
	if len(queryTerms) == 0 {
		return 0
	}
	// The user naming the skill is not a guess.
	if name := strings.ToLower(strings.TrimSpace(s.Name)); name != "" {
		if strings.Contains(strings.ToLower(query), name) {
			return scoreNamedOutright
		}
	}

	st := c.terms[s]
	var matched, total float64
	seen := make(map[string]bool, len(queryTerms))
	for _, term := range queryTerms {
		if seen[term] {
			continue
		}
		seen[term] = true
		idf := c.idfOf(term)
		// The most a term could contribute if it appeared in the strongest field.
		total += fieldWeightName * idf
		if weight, ok := st.byTerm[term]; ok && c.distinguishes(term) {
			matched += weight * idf
		}
	}
	if total == 0 {
		return 0
	}
	relevance := matched / total

	// A skill that declares path globs and sees one of them in play is telling
	// us it applies, in its own terms rather than ours.
	if len(s.Paths) > 0 && len(touchedPaths) > 0 && s.MatchesAnyPath(touchedPaths) && relevance < scorePathMatch {
		relevance = scorePathMatch
	}
	if relevance > 1 {
		relevance = 1
	}
	return relevance
}

// resolveScored is the shared implementation behind ResolveForModel and
// ResolveForModelScored. The caller holds the read lock.
func (r *Registry) resolveScored(input string, opts ResolveOptions) []ScoredSkill {
	query := strings.TrimSpace(input)
	queryTerms := tokenize(query)

	candidates := make([]*Skill, 0, len(r.byName))
	for _, s := range r.byName {
		if !s.IsModelInvocable() {
			continue
		}
		if !s.MatchesAnyPath(opts.TouchedPaths) {
			continue
		}
		candidates = append(candidates, s)
	}

	c := buildCorpus(candidates)
	scored := make([]ScoredSkill, 0, len(candidates))
	for _, s := range candidates {
		score := c.score(s, query, queryTerms, opts.TouchedPaths)
		if score <= 0 || score < opts.MinScore {
			continue
		}
		scored = append(scored, ScoredSkill{Skill: s, Score: score})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].Skill.Name < scored[j].Skill.Name
		}
		return scored[i].Score > scored[j].Score
	})
	if opts.Limit > 0 && len(scored) > opts.Limit {
		scored = scored[:opts.Limit]
	}
	return scored
}
