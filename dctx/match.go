package dctx

import (
	"fmt"

	"github.com/moby/patternmatcher"
)

// matcher wraps the authoritative patternmatcher with per-rule matchers used
// only for attribution ("which line decided this path?").
//
// All matching decisions come from patternmatcher itself. The per-rule
// matchers below re-run the same MatchesOrParentMatches call one pattern at a
// time; they never reimplement pattern semantics.
type matcher struct {
	pm    *patternmatcher.PatternMatcher
	rules []Rule
	// perRule[i] matches rule i alone, with any "!" stripped so that a negated
	// rule still reports where it applies.
	perRule []*patternmatcher.PatternMatcher
}

func newMatcher(f *IgnoreFile) (*matcher, error) {
	pm, err := patternmatcher.New(f.Patterns())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", displayName(f.Name), err)
	}
	m := &matcher{pm: pm, rules: f.Rules}
	for _, r := range f.Rules {
		single, err := patternmatcher.New([]string{r.Pattern})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", displayName(f.Name), err)
		}
		m.perRule = append(m.perRule, single)
	}
	return m, nil
}

func displayName(name string) string {
	if name == "" {
		return ".dockerignore"
	}
	return name
}

// hasExclusions reports whether any rule re-includes paths. When false, an
// ignored directory can be skipped wholesale because nothing inside it can
// come back.
func (m *matcher) hasExclusions() bool { return m.pm.Exclusions() }

// matching returns the indices of every rule that applies to path, in file
// order. The last one is decisive: patternmatcher evaluates rules in order and
// each match sets the result to !exclusion, so later rules override earlier
// ones.
func (m *matcher) matching(path string) ([]int, error) {
	var out []int
	for i, single := range m.perRule {
		ok, err := single.MatchesOrParentMatches(path)
		if err != nil {
			return nil, fmt.Errorf("rule %q: %w", m.rules[i].Raw, err)
		}
		if ok {
			out = append(out, i)
		}
	}
	return out, nil
}

// decide returns the rule that determined path's fate, or nil if no rule
// applied.
func (m *matcher) decide(path string) (*Rule, error) {
	idx, err := m.matching(path)
	if err != nil {
		return nil, err
	}
	if len(idx) == 0 {
		return nil, nil
	}
	r := m.rules[idx[len(idx)-1]]
	return &r, nil
}
