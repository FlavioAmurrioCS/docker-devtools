package dctx

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseRulesRecoversLineNumbers(t *testing.T) {
	// Comments and blank lines are dropped by ignorefile.ReadAll, so the
	// surviving patterns must still cite the line they came from.
	input := []byte("# a comment\n\nnode_modules\n\n  *.log  \n!keep.log\n")
	rules, err := parseRules(input)
	if err != nil {
		t.Fatal(err)
	}
	want := []Rule{
		{Raw: "node_modules", Pattern: "node_modules", Line: 3},
		{Raw: "*.log", Pattern: "*.log", Line: 5},
		{Raw: "!keep.log", Pattern: "keep.log", Negated: true, Line: 6},
	}
	if !reflect.DeepEqual(rules, want) {
		t.Errorf("parseRules() =\n%#v\nwant\n%#v", rules, want)
	}
}

func TestParseRulesStripsBOM(t *testing.T) {
	rules, err := parseRules([]byte("\xEF\xBB\xBFnode_modules\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Raw != "node_modules" {
		t.Errorf("parseRules() = %#v, want the BOM stripped", rules)
	}
}

// resolve is the two-step every caller does: locate the Dockerfile, then load
// the ignore file derived from it.
func resolve(t *testing.T, contextDir, flagValue string) *IgnoreFile {
	t.Helper()
	df, err := ResolveDockerfile(contextDir, flagValue)
	if err != nil {
		t.Fatal(err)
	}
	f, err := LoadIgnoreFile(contextDir, df)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPerDockerfileIgnoreFileWins(t *testing.T) {
	dir := filepath.Join("..", "testdata", "dctx", "perdockerfile", "context")
	if f := resolve(t, dir, filepath.Join(dir, "Prj1")); f.Name != "Prj1.dockerignore" {
		t.Errorf("ignore file = %q, want Prj1.dockerignore", f.Name)
	}

	// With no -f, the default Dockerfile is absent here, so .dockerignore wins.
	if f := resolve(t, dir, ""); f.Name != ".dockerignore" {
		t.Errorf("ignore file = %q, want .dockerignore", f.Name)
	}
}

func TestDockerfileFlagIsRelativeToTheWorkingDirectory(t *testing.T) {
	// docker build resolves -f from the current directory, not the context
	// (docker/cli cli/command/image/build/context.go). The fixture's Dockerfile
	// sits outside its context, so a context-relative reading cannot find it
	// and the two spellings give different answers.
	dir := filepath.Join("..", "testdata", "dctx", "outofcontext")
	ctxDir := filepath.Join(dir, "context")

	// Reported relative to the working directory, because it is not in the
	// context to be relative to.
	want := filepath.ToSlash(filepath.Join(dir, "build.Dockerfile.dockerignore"))
	f := resolve(t, ctxDir, filepath.Join(dir, "build.Dockerfile"))
	if f.Name != want {
		t.Errorf("ignore file = %q, want %q, the one beside the Dockerfile", f.Name, want)
	}
	if len(f.Rules) != 1 || f.Rules[0].Raw != "beside-only" {
		t.Errorf("rules = %#v, want the out-of-context file's contents", f.Rules)
	}
}

func TestExplicitDockerfileMustExist(t *testing.T) {
	// docker build fails with "failed to read dockerfile: open ...: no such
	// file or directory" rather than building something else.
	dir := filepath.Join("..", "testdata", "dctx", "perdockerfile", "context")

	if _, err := ResolveDockerfile(dir, filepath.Join(dir, "Nope")); err == nil {
		t.Fatal("ResolveDockerfile() = nil error, want a failure for a missing -f")
	}

	// A value that resolves only inside the context is the pre-0.1 spelling.
	// Failing is right, but the error has to say what to pass instead.
	_, err := ResolveDockerfile(dir, "Prj1")
	if err == nil {
		t.Fatal("ResolveDockerfile() = nil error, want a failure for a context-relative -f")
	}
	if !strings.Contains(err.Error(), filepath.Join(dir, "Prj1")) {
		t.Errorf("error = %v, want it to name the path to pass instead", err)
	}
}

func TestResolveDockerfileAcceptsLowercase(t *testing.T) {
	// BuildKit also accepts a lowercase "dockerfile" (moby/moby#10858), and
	// its ignore file is then "dockerfile.dockerignore".
	//
	// The answer has to be the same on a case-sensitive filesystem and on a
	// case-insensitive one, which is why resolution reads the directory
	// listing instead of stat'ing each candidate.
	dir := filepath.Join("..", "testdata", "dctx", "lowercase", "context")

	df, err := ResolveDockerfile(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if df.Display != "dockerfile" {
		t.Fatalf("ResolveDockerfile() = %q, want dockerfile", df.Display)
	}

	f := resolve(t, dir, "")
	if f.Name != "dockerfile.dockerignore" {
		t.Errorf("ignore file = %q, want dockerfile.dockerignore", f.Name)
	}
	if len(f.Rules) != 1 || f.Rules[0].Raw != "secret" {
		t.Errorf("rules = %#v, want the per-Dockerfile ignore file's contents", f.Rules)
	}
}

func TestLowercaseFallbackAppliesToAnExplicitFlag(t *testing.T) {
	// buildx runs the same fallback on an explicit -f, guarded on the base name
	// (build/opt.go handleLowercaseDockerfile), and so does the frontend
	// (buildkit frontend/dockerui/config.go). So "-f <dir>/Dockerfile" against a
	// directory holding only "dockerfile" still finds dockerfile.dockerignore.
	dir := filepath.Join("..", "testdata", "dctx", "lowercase", "context")

	f := resolve(t, dir, filepath.Join(dir, "Dockerfile"))
	if f.Name != "dockerfile.dockerignore" {
		t.Errorf("ignore file = %q, want dockerfile.dockerignore", f.Name)
	}
}

func TestPercentInIgnoreFileNameIsNotAFormatVerb(t *testing.T) {
	// The original tool built errors with fmt.Errorf(name+": %w"), so a name
	// containing % corrupted the message. Loading must succeed and report the
	// name verbatim.
	dir := filepath.Join("..", "testdata", "dctx", "percent", "context")
	f := resolve(t, dir, filepath.Join(dir, "we%ird"))
	if f.Name != "we%ird.dockerignore" {
		t.Errorf("ignore file = %q, want we%%ird.dockerignore", f.Name)
	}
}

func TestMissingIgnoreFileIsNotAnError(t *testing.T) {
	dir := filepath.Join("..", "testdata", "dctx", "none", "context")
	f := resolve(t, dir, "")
	if f.Name != "" || len(f.Rules) != 0 {
		t.Errorf("LoadIgnoreFile() = %#v, want an empty result", f)
	}
}

func TestCheckContextDirRejectsAFile(t *testing.T) {
	// "build-context ls path/to/Dockerfile" is the natural mistake, because the
	// image-refs commands do take file paths. Before this check it failed with
	// "open .../Dockerfile/Dockerfile.dockerignore: not a directory", naming a
	// path the caller never mentioned.
	file := filepath.Join("..", "testdata", "dctx", "none", "context", "Dockerfile")
	err := CheckContextDir(file)
	if err == nil {
		t.Fatal("CheckContextDir() = nil, want an error for a regular file")
	}
	if !strings.Contains(err.Error(), "context must be a directory") {
		t.Errorf("CheckContextDir() = %v, want docker build's own wording", err)
	}
}
