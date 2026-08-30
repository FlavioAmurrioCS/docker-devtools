// Package dctx lists the files Docker sends to the daemon as a build context.
//
// Ignore-file parsing, path matching and tree traversal are delegated to the
// packages Docker and BuildKit use themselves, so results match docker build
// rather than approximating it.
package dctx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MatchedRule is one ignore-file rule that applies to an explained path.
type MatchedRule struct {
	Rule    string `json:"rule"`
	Line    int    `json:"line"`
	Negated bool   `json:"negated"`
	// Decisive marks the last matching rule, which determines the outcome.
	Decisive bool `json:"decisive"`
}

// Explanation records why a single path is included or ignored.
type Explanation struct {
	Schema     int           `json:"schema"`
	Context    string        `json:"context"`
	Dockerfile string        `json:"dockerfile"`
	Ignorefile string        `json:"ignorefile"`
	Path       string        `json:"path"`
	Status     Status        `json:"status"`
	Exists     bool          `json:"exists"`
	Rules      []MatchedRule `json:"rules"`
}

// Explain reports which ignore-file rules apply to target and which one
// decides its fate.
//
// target may be absolute or relative to the context directory. It need not
// exist: explaining a hypothetical path is useful when editing .dockerignore.
func Explain(opt Options, target string) (*Explanation, error) {
	contextDir, err := filepath.Abs(opt.Context)
	if err != nil {
		return nil, err
	}

	rel, err := relToContext(contextDir, target)
	if err != nil {
		return nil, err
	}

	dockerfile := ResolveDockerfile(contextDir, opt.Dockerfile)
	ignoreFile, err := LoadIgnoreFile(contextDir, dockerfile)
	if err != nil {
		return nil, err
	}
	m, err := newMatcher(ignoreFile)
	if err != nil {
		return nil, err
	}

	// The status comes from patternmatcher itself, never from the per-rule
	// attribution below.
	ignored, err := m.pm.MatchesOrParentMatches(rel)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}

	idx, err := m.matching(rel)
	if err != nil {
		return nil, err
	}
	rules := make([]MatchedRule, len(idx))
	for i, ri := range idx {
		r := m.rules[ri]
		rules[i] = MatchedRule{
			Rule:     r.Raw,
			Line:     r.Line,
			Negated:  r.Negated,
			Decisive: i == len(idx)-1,
		}
	}

	exists := true
	if _, err := os.Lstat(filepath.Join(contextDir, rel)); errors.Is(err, os.ErrNotExist) {
		exists = false
	}

	return &Explanation{
		Schema:     SchemaVersion,
		Context:    contextDir,
		Dockerfile: dockerfile,
		Ignorefile: ignoreFile.Name,
		Path:       filepath.ToSlash(rel),
		Status:     statusOf(ignored),
		Exists:     exists,
		Rules:      rules,
	}, nil
}

// relToContext normalizes target to a clean path relative to contextDir.
func relToContext(contextDir, target string) (string, error) {
	t := target
	if filepath.IsAbs(t) {
		rel, err := filepath.Rel(contextDir, t)
		if err != nil {
			return "", err
		}
		t = rel
	}
	t = filepath.Clean(filepath.FromSlash(t))
	if t == "." || t == string(filepath.Separator) {
		return "", errors.New("cannot explain the context root itself")
	}
	if t == ".." || strings.HasPrefix(t, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the build context", target)
	}
	return t, nil
}
