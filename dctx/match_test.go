package dctx

import (
	"io/fs"
	"path/filepath"
	"testing"
)

// TestAttributionAgreesWithMatcher is the safety net under the "last matching
// rule wins" shortcut in match.go.
//
// patternmatcher skips a rule whenever pattern.exclusion != matched, which can
// only skip rules that could not have changed the outcome. So iterating every
// rule and taking the last match must agree with the real matcher on every
// path. If upstream ever changes that, this fails loudly rather than silently
// mislabelling which line was responsible.
func TestAttributionAgreesWithMatcher(t *testing.T) {
	for _, dir := range fixtures(t) {
		name := filepath.Base(dir)
		t.Run(name, func(t *testing.T) {
			ctxDir := filepath.Join(dir, "context")
			c := loadCase(t, dir)
			dockerfile := ResolveDockerfile(ctxDir, c.Dockerfile)

			ignoreFile, err := LoadIgnoreFile(ctxDir, dockerfile)
			if err != nil {
				t.Fatal(err)
			}
			m, err := newMatcher(ignoreFile)
			if err != nil {
				t.Fatal(err)
			}

			err = filepath.WalkDir(ctxDir, func(path string, _ fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				rel, err := filepath.Rel(ctxDir, path)
				if err != nil || rel == "." {
					return err
				}

				want, err := m.pm.MatchesOrParentMatches(rel)
				if err != nil {
					return err
				}

				rule, err := m.decide(rel)
				if err != nil {
					return err
				}
				got := rule != nil && !rule.Negated

				if got != want {
					t.Errorf("%s: attribution says ignored=%v, matcher says %v (rule %v)",
						rel, got, want, rule)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMatcherReportsExclusions(t *testing.T) {
	tests := []struct {
		name  string
		rules []string
		want  bool
	}{
		{"no negation", []string{"node_modules", "*.log"}, false},
		{"with negation", []string{"node_modules", "!node_modules/keep"}, true},
		{"empty", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &IgnoreFile{}
			for i, raw := range tt.rules {
				r := Rule{Raw: raw, Pattern: raw, Line: i + 1}
				if raw[0] == '!' {
					r.Negated = true
					r.Pattern = raw[1:]
				}
				f.Rules = append(f.Rules, r)
			}
			m, err := newMatcher(f)
			if err != nil {
				t.Fatal(err)
			}
			if got := m.hasExclusions(); got != tt.want {
				t.Errorf("hasExclusions() = %v, want %v", got, tt.want)
			}
		})
	}
}
