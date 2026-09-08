package spec

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// LearnQueryFamilyExample is one query string the recall path will
// normalize into a QueryFamily key, plus a source label for errors
// (generated test fixture name or an authored playbook path).
type LearnQueryFamilyExample struct {
	Source string
	Query  string
}

// learnSeededQueryFamilyExamples are the query_family_examples the
// generator always emits into the printed CLI's learn/playbook tests.
// A ticker_pattern that classifies every remaining content token in
// these as a ticker makes QueryFamily empty, so playbook init skips
// every seed as unreachable. Kept here so spec.Validate fails closed
// before generate ships that dead surface. Must stay in lockstep with
// the templates named in LearnSeededQueryFamilyQueries.
var learnSeededQueryFamilyExamples = []LearnQueryFamilyExample{
	{Source: "playbook_init_test.go", Query: "alpha example query"},
	{Source: "playbook_init_test.go", Query: "alpha second phrasing"},
	{Source: "playbook_init_test.go", Query: "beta example query"},
	{Source: "playbook_init_test.go", Query: "beta second phrasing"},
	{Source: "playbook_init_test.go", Query: "valid query family example"},
	{Source: "teach_playbook_test.go", Query: "foo bar baz"},
	{Source: "playbooks_test.go", Query: "how did $X end the season"},
	{Source: "recall_canonical_test.go", Query: "report Alpha widget today"},
}

// LearnSeededQueryFamilyQueries returns the query strings the generator
// embeds as query_family_examples / seeded recall examples. The generator
// test suite greps templates for these so the spec-time check cannot
// drift from what generate actually emits.
func LearnSeededQueryFamilyQueries() []string {
	out := make([]string, 0, len(learnSeededQueryFamilyExamples))
	for _, ex := range learnSeededQueryFamilyExamples {
		out = append(out, ex.Query)
	}
	return out
}

// learnDefaultStopwords mirrors the printed CLI's entities.defaultStopwords.
// QueryFamily is the bag of non-entity, non-ticker, non-stopword tokens;
// dropping the same words here keeps the reachability check aligned with
// recall-time Extract rather than treating filler as a surviving family.
var learnDefaultStopwords = map[string]struct{}{
	"a": {}, "an": {}, "the": {},
	"is": {}, "are": {}, "was": {}, "were": {}, "be": {}, "been": {}, "being": {},
	"of": {}, "to": {}, "in": {}, "on": {}, "at": {}, "for": {}, "with": {}, "from": {}, "by": {}, "about": {},
	"what": {}, "which": {}, "who": {}, "whom": {}, "whose": {}, "how": {}, "when": {}, "why": {}, "where": {},
	"will": {}, "would": {}, "could": {}, "should": {}, "may": {}, "might": {}, "can": {}, "shall": {},
	"do": {}, "does": {}, "did": {}, "have": {}, "has": {}, "had": {},
	"and": {}, "or": {}, "but": {}, "if": {}, "then": {}, "than": {},
	"this": {}, "that": {}, "these": {}, "those": {}, "it": {}, "its": {},
}

// CheckLearnQueryFamilyReachability reports whether learn.TickerPatterns
// would make QueryFamily empty for any seeded generator example or any
// extra authored playbook example. No-op when the loop will not be emitted
// (disabled, or legacy enabled: false) or no ticker patterns are declared.
func CheckLearnQueryFamilyReachability(learn *LearnConfig, extras []LearnQueryFamilyExample) error {
	if !learnLoopEmits(learn) || len(learn.TickerPatterns) == 0 {
		return nil
	}
	examples := make([]LearnQueryFamilyExample, 0, len(learnSeededQueryFamilyExamples)+len(extras))
	examples = append(examples, learnSeededQueryFamilyExamples...)
	examples = append(examples, extras...)
	return learn.queryFamilyReachabilityError(examples)
}

func learnLoopEmits(c *LearnConfig) bool {
	if c == nil || c.Disabled {
		return false
	}
	if c.EnabledSet {
		return c.Enabled
	}
	return true
}

func (c *LearnConfig) queryFamilyReachabilityError(examples []LearnQueryFamilyExample) error {
	if !learnLoopEmits(c) || len(c.TickerPatterns) == 0 {
		return nil
	}
	compiled := make([]*regexp.Regexp, len(c.TickerPatterns))
	for i, pattern := range c.TickerPatterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("learn.ticker_patterns[%d] is not a valid Go regexp: %w", i, err)
		}
		compiled[i] = re
	}
	stopwords := make(map[string]struct{}, len(learnDefaultStopwords)+len(c.Stopwords))
	for w := range learnDefaultStopwords {
		stopwords[w] = struct{}{}
	}
	for _, w := range c.Stopwords {
		w = strings.ToLower(strings.TrimSpace(w))
		if w != "" {
			stopwords[w] = struct{}{}
		}
	}
	for _, ex := range examples {
		query := strings.TrimSpace(ex.Query)
		if query == "" {
			continue
		}
		without := learnNonEntityTokens(query, nil, stopwords)
		if len(without) == 0 {
			continue
		}
		claimed := make(map[int][]string)
		with := learnNonEntityTokens(query, compiled, stopwords)
		if len(with) > 0 {
			continue
		}
		for _, tok := range without {
			if idx, ok := learnTickerIndex(tok, compiled); ok {
				claimed[idx] = append(claimed[idx], tok)
			}
		}
		if len(claimed) == 0 {
			continue
		}
		idx := claimedPatternIndex(claimed)
		source := strings.TrimSpace(ex.Source)
		if source == "" {
			source = "query_family_example"
		}
		return fmt.Errorf("learn.ticker_patterns[%d] (%q) classifies every remaining query-family token in %s example %q as a ticker (claimed %s); QueryFamily would be empty and seeded playbooks would be unreachable at recall time. Narrow the pattern so ordinary query words stay non-entity tokens (require a distinctive prefix, separator, or uppercase)",
			idx, c.TickerPatterns[idx], source, query, strings.Join(claimed[idx], ", "))
	}
	return nil
}

func claimedPatternIndex(claimed map[int][]string) int {
	best := -1
	for idx := range claimed {
		if best < 0 || idx < best {
			best = idx
		}
	}
	return best
}

// learnNonEntityTokens mirrors entities.Extract's non-entity remainder:
// ticker match (when patterns are provided) wins, then ALL-CAPS / capitalized
// tokens become entities, stopwords drop, and leftover lowercase tokens are
// the QueryFamily bag. Classification is duplicated from the learn extract
// template so spec/generate-time validation sees the same empty-family
// outcome recall will.
func learnNonEntityTokens(query string, tickers []*regexp.Regexp, stopwords map[string]struct{}) []string {
	var family []string
	for raw := range strings.FieldsSeq(query) {
		tok := learnTrimPunct(raw)
		if tok == "" {
			continue
		}
		if _, ok := learnTickerIndex(tok, tickers); ok {
			continue
		}
		lower := strings.ToLower(tok)
		if (len(tok) >= 2 && learnIsAllCaps(tok)) || learnIsCapitalized(tok) {
			continue
		}
		if _, isStopword := stopwords[lower]; isStopword {
			continue
		}
		family = append(family, lower)
	}
	return family
}

func learnTickerIndex(token string, tickers []*regexp.Regexp) (int, bool) {
	for i, re := range tickers {
		if re != nil && re.MatchString(token) {
			return i, true
		}
	}
	return -1, false
}

func learnTrimPunct(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		switch r {
		case '.', ',', '?', '!', ':', ';', '\'', '"', '(', ')', '[', ']', '{', '}':
			return true
		}
		return false
	})
}

func learnIsAllCaps(s string) bool {
	hasLetter := false
	for _, r := range s {
		if unicode.IsLetter(r) {
			hasLetter = true
			if !unicode.IsUpper(r) {
				return false
			}
		}
	}
	return hasLetter
}

func learnIsCapitalized(s string) bool {
	firstUpper := false
	hasLower := false
	for i, r := range s {
		if i == 0 {
			if unicode.IsLetter(r) && unicode.IsUpper(r) {
				firstUpper = true
				continue
			}
			return false
		}
		if unicode.IsLetter(r) && unicode.IsLower(r) {
			hasLower = true
		}
	}
	return firstUpper && hasLower
}

// ParsePlaybookQueryFamilyExamples decodes query_family_examples from a
// playbook JSON blob. Other fields are ignored so this can run against
// authored embeds without importing the generated learn package.
func ParsePlaybookQueryFamilyExamples(data []byte) ([]string, error) {
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 {
		return nil, fmt.Errorf("empty playbook")
	}
	var parsed struct {
		QueryFamilyExamples []string `json:"query_family_examples"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	return parsed.QueryFamilyExamples, nil
}
