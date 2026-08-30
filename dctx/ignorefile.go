package dctx

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/moby/patternmatcher/ignorefile"
)

// DefaultDockerfileName is the filename BuildKit looks for when -f is not given.
const DefaultDockerfileName = "Dockerfile"

// Rule is a single pattern from an ignore file, kept alongside the source line
// it came from so diagnostics can cite ".dockerignore:12".
type Rule struct {
	// Raw is the pattern exactly as ignorefile.ReadAll normalized it,
	// including any leading "!".
	Raw string
	// Pattern is Raw with the "!" prefix removed.
	Pattern string
	// Negated reports whether this rule re-includes rather than excludes.
	Negated bool
	// Line is the 1-based line number in the ignore file.
	Line int
}

// IgnoreFile is a parsed .dockerignore.
type IgnoreFile struct {
	// Name is the ignore file used, relative to the context directory, or ""
	// when no ignore file was found.
	Name string
	// Rules are the patterns in file order.
	Rules []Rule
}

// Patterns returns the raw patterns in file order, suitable for
// patternmatcher.New.
func (f *IgnoreFile) Patterns() []string {
	if f == nil {
		return nil
	}
	out := make([]string, len(f.Rules))
	for i, r := range f.Rules {
		out[i] = r.Raw
	}
	return out
}

// ResolveDockerfile reports the Dockerfile name to derive the ignore file from.
//
// When flagValue is empty this mirrors BuildKit, which looks for "Dockerfile"
// and also accepts the lowercase spelling (moby/moby#10858). The returned name
// need not exist: it is only used to derive "<name>.dockerignore".
//
// The candidates are compared against the directory listing rather than
// stat'ed, so the answer does not depend on whether the filesystem is
// case-sensitive. os.Stat("Dockerfile") succeeds on a macOS volume holding
// only "dockerfile", which would report a different name than the same tree
// on Linux. Both spellings open the same file there either way, so this
// changes only what gets reported, not which file is used.
func ResolveDockerfile(contextDir, flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	lower := strings.ToLower(DefaultDockerfileName)
	entries, err := os.ReadDir(contextDir)
	if err != nil {
		return DefaultDockerfileName
	}
	found := ""
	for _, e := range entries {
		switch e.Name() {
		case DefaultDockerfileName:
			return DefaultDockerfileName
		case lower:
			found = lower
		}
	}
	if found != "" {
		return found
	}
	return DefaultDockerfileName
}

// LoadIgnoreFile finds and parses the ignore file for a build.
//
// Candidates are tried in BuildKit's order: "<dockerfile>.dockerignore" first,
// then ".dockerignore" in the context directory
// (moby/buildkit/frontend/dockerui/config.go). dockerfile may be an absolute
// path, in which case the per-Dockerfile candidate sits beside it rather than
// inside the context.
func LoadIgnoreFile(contextDir, dockerfile string) (*IgnoreFile, error) {
	for _, candidate := range ignoreCandidates(contextDir, dockerfile) {
		data, err := os.ReadFile(candidate.path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		rules, err := parseRules(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", candidate.name, err)
		}
		return &IgnoreFile{Name: candidate.name, Rules: rules}, nil
	}
	return &IgnoreFile{}, nil
}

type candidate struct {
	// path is what we open; name is what we report to the user.
	path, name string
}

func ignoreCandidates(contextDir, dockerfile string) []candidate {
	perDockerfile := dockerfile + ".dockerignore"
	name := perDockerfile
	path := perDockerfile
	if !filepath.IsAbs(perDockerfile) {
		path = filepath.Join(contextDir, perDockerfile)
	} else {
		// An absolute -f lives outside the context; report it as given.
		name = filepath.Base(perDockerfile)
	}
	return []candidate{
		{path: path, name: name},
		{path: filepath.Join(contextDir, ".dockerignore"), name: ".dockerignore"},
	}
}

// parseRules delegates pattern parsing to ignorefile.ReadAll — the same
// function docker/cli and BuildKit use — and separately recovers the source
// line of each returned pattern.
//
// ReadAll drops comment and blank lines and keeps everything else in order, so
// replaying only that skip rule maps patterns back to lines exactly. No
// pattern normalization is duplicated here.
func parseRules(data []byte) ([]Rule, error) {
	patterns, err := ignorefile.ReadAll(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	lines := patternLines(data)
	if len(lines) != len(patterns) {
		// Should be unreachable; fall back to unknown lines rather than
		// mislabelling them.
		lines = make([]int, len(patterns))
	}

	rules := make([]Rule, len(patterns))
	for i, p := range patterns {
		r := Rule{Raw: p, Pattern: p, Line: lines[i]}
		if strings.HasPrefix(p, "!") {
			r.Negated = true
			r.Pattern = p[1:]
		}
		rules[i] = r
	}
	return rules, nil
}

// patternLines returns the 1-based line number of each line ReadAll would keep.
func patternLines(data []byte) []int {
	var out []int
	utf8bom := []byte{0xEF, 0xBB, 0xBF}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	line := 0
	for scanner.Scan() {
		b := scanner.Bytes()
		if line == 0 {
			b = bytes.TrimPrefix(b, utf8bom)
		}
		line++
		if bytes.HasPrefix(b, []byte("#")) {
			continue
		}
		if len(bytes.TrimSpace(b)) == 0 {
			continue
		}
		out = append(out, line)
	}
	if scanner.Err() != nil {
		return nil
	}
	return out
}
