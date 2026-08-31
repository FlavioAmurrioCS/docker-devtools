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

// DockerfileRef is the Dockerfile a build would read, and how to talk about it.
type DockerfileRef struct {
	// Path is absolute. When Explicit is false it need not exist: only
	// "<Path>.dockerignore" is ever opened.
	Path string
	// Display is what to report: relative to the context when the file is
	// inside it, and the value as the caller spelled it when it is not.
	Display string
	// Explicit reports whether the path came from a -f value.
	Explicit bool
}

// ResolveDockerfile locates the Dockerfile a build would read.
//
// flagValue is the -f/--file value, resolved from the current working directory
// rather than the context, which is what docker build does. docker/cli says so
// outright (cli/command/image/build/context.go:262): "When using a local context
// directory, and the Dockerfile is specified with the `-f/--file` option then it
// is considered relative to the current directory and not the context
// directory." Empty means the default in the context root.
//
// An explicit value must exist, as it must for docker build, which fails with
// "failed to read dockerfile: open ...: no such file or directory". A value that
// only resolves inside the context is the pre-0.1 spelling, so say what to pass
// instead rather than just failing.
func ResolveDockerfile(contextDir, flagValue string) (DockerfileRef, error) {
	// Both paths are made absolute before anything is compared: a caller may
	// pass either spelling, and Display is computed with filepath.Rel, which
	// gives nonsense when one side is relative and the other is not.
	// Kept for diagnostics: the hint below has to echo the spelling the caller
	// used, not an absolute path they never typed.
	givenContext := contextDir
	contextDir, err := filepath.Abs(contextDir)
	if err != nil {
		return DockerfileRef{}, err
	}

	if flagValue == "" {
		// The default must be there, as it must for docker build, which fails
		// with "failed to read dockerfile: open Dockerfile: no such file or
		// directory". Listing a context for a build that cannot run describes
		// nothing, and the -f path already holds to this rule.
		name := lowercaseFallback(contextDir, DefaultDockerfileName)
		path := filepath.Join(contextDir, name)
		if !statOK(path) {
			return DockerfileRef{}, fmt.Errorf(
				"no %s in %s; pass -f to name one", DefaultDockerfileName, givenContext)
		}
		return DockerfileRef{Path: path, Display: name}, nil
	}

	abs, err := filepath.Abs(flagValue)
	if err != nil {
		return DockerfileRef{}, err
	}
	// buildx applies the lowercase fallback to an explicit -f too, guarded on
	// the base name (build/opt.go handleLowercaseDockerfile), and so does the
	// frontend (buildkit frontend/dockerui/config.go).
	abs = filepath.Join(filepath.Dir(abs), lowercaseFallback(filepath.Dir(abs), filepath.Base(abs)))

	if _, err := os.Stat(abs); err != nil {
		if statOK(filepath.Join(contextDir, flagValue)) {
			return DockerfileRef{}, fmt.Errorf(
				"dockerfile %s: no such file. The path is relative to the current directory, "+
					"as it is for docker build; did you mean %s?",
				flagValue, filepath.Join(givenContext, flagValue))
		}
		if errors.Is(err, fs.ErrNotExist) {
			return DockerfileRef{}, fmt.Errorf("dockerfile %s: no such file or directory", flagValue)
		}
		return DockerfileRef{}, fmt.Errorf("dockerfile %s: %w", flagValue, err)
	}

	return DockerfileRef{Path: abs, Display: displayPath(contextDir, abs), Explicit: true}, nil
}

// lowercaseFallback returns "dockerfile" when dir holds that spelling and not
// "Dockerfile" (moby/moby#10858). Any other name is returned untouched, which
// is the same guard buildx and the frontend both apply.
//
// Those two spellings are the whole candidate set, because they are all
// buildkit looks for (frontend/dockerui: DefaultDockerfileName plus the
// lowercase form). There is deliberately no Containerfile fallback here even
// though imgref.FileKind accepts the name: what docker opens by default and
// what is worth scanning for references are different questions.
//
// Candidates are compared against the directory listing rather than stat'ed, so
// the answer does not depend on whether the filesystem is case-sensitive.
// os.Stat("Dockerfile") succeeds on a macOS volume holding only "dockerfile",
// which would report a different name than the same tree on Linux. Both
// spellings open the same file there either way, so this changes only what gets
// reported, not which file is used.
func lowercaseFallback(dir, name string) string {
	if name != DefaultDockerfileName {
		return name
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return name
	}
	lower := strings.ToLower(DefaultDockerfileName)
	found := ""
	for _, e := range entries {
		switch e.Name() {
		case DefaultDockerfileName:
			return name
		case lower:
			found = lower
		}
	}
	if found != "" {
		return found
	}
	return name
}

// displayPath keeps a reported path readable and machine-independent: relative
// to the context when the file is inside it, and relative to the working
// directory when it is not, which is how the caller named it in the first place.
//
// It is derived from the resolved path rather than the given one, so a name the
// lowercase fallback rewrote is reported as the file that was actually read.
func displayPath(contextDir, abs string) string {
	if rel, err := filepath.Rel(contextDir, abs); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	if cwd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(cwd, abs); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(abs)
}

func statOK(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// CheckContextDir rejects a context that is not a directory.
//
// Without it the first failure is whatever LoadIgnoreFile or fsutil reports:
// "build-context ls path/to/Dockerfile" fails with
// "open path/to/Dockerfile/Dockerfile.dockerignore: not a directory", naming a
// file the caller never mentioned. docker build says "context must be a
// directory", so say that.
func CheckContextDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("context must be a directory: %s", dir)
	}
	return nil
}

// LoadIgnoreFile finds and parses the ignore file for a build.
//
// Candidates are tried in BuildKit's order: "<dockerfile>.dockerignore" beside
// the Dockerfile first, then ".dockerignore" in the context directory
// (moby/buildkit frontend/dockerui/config.go). The first wins outright: the
// frontend loads the context-root file only when the per-Dockerfile one came up
// empty, so these replace rather than merge.
//
// The Dockerfile may sit outside the context, because buildx mounts its
// directory as a separate "dockerfile" input (buildx build/opt.go).
func LoadIgnoreFile(contextDir string, df DockerfileRef) (*IgnoreFile, error) {
	for _, c := range ignoreCandidates(contextDir, df) {
		data, err := os.ReadFile(c.path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		rules, err := parseRules(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c.name, err)
		}
		return &IgnoreFile{Name: c.name, Rules: rules}, nil
	}
	return &IgnoreFile{}, nil
}

type candidate struct {
	// path is what we open; name is what we report to the user.
	path, name string
}

func ignoreCandidates(contextDir string, df DockerfileRef) []candidate {
	const suffix = ".dockerignore"
	return []candidate{
		{path: df.Path + suffix, name: df.Display + suffix},
		{path: filepath.Join(contextDir, suffix), name: suffix},
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
