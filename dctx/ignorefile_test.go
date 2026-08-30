package dctx

import (
	"os"
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

func TestPerDockerfileIgnoreFileWins(t *testing.T) {
	dir := filepath.Join("..", "testdata", "dctx", "perdockerfile", "context")
	f, err := LoadIgnoreFile(dir, "Prj1")
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "Prj1.dockerignore" {
		t.Errorf("ignore file = %q, want Prj1.dockerignore", f.Name)
	}

	// With no -f, the default Dockerfile is absent here, so .dockerignore wins.
	f, err = LoadIgnoreFile(dir, ResolveDockerfile(dir, ""))
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != ".dockerignore" {
		t.Errorf("ignore file = %q, want .dockerignore", f.Name)
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

	got := ResolveDockerfile(dir, "")
	if got != "dockerfile" {
		t.Fatalf("ResolveDockerfile() = %q, want dockerfile", got)
	}

	f, err := LoadIgnoreFile(dir, got)
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "dockerfile.dockerignore" {
		t.Errorf("ignore file = %q, want dockerfile.dockerignore", f.Name)
	}
	if len(f.Rules) != 1 || f.Rules[0].Raw != "secret" {
		t.Errorf("rules = %#v, want the per-Dockerfile ignore file's contents", f.Rules)
	}
}

func TestResolveDockerfileHonoursExplicitFlag(t *testing.T) {
	dir := filepath.Join("..", "testdata", "dctx", "lowercase", "context")
	if got := ResolveDockerfile(dir, "Other"); got != "Other" {
		t.Errorf("ResolveDockerfile() = %q, want Other", got)
	}
}

func TestPercentInIgnoreFileNameIsNotAFormatVerb(t *testing.T) {
	// The original tool built errors with fmt.Errorf(name+": %w"), so a name
	// containing % corrupted the message. Loading must succeed and report the
	// name verbatim.
	dir := filepath.Join("..", "testdata", "dctx", "percent", "context")
	f, err := LoadIgnoreFile(dir, "we%ird")
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "we%ird.dockerignore" {
		t.Errorf("ignore file = %q, want we%%ird.dockerignore", f.Name)
	}
}

func TestMissingIgnoreFileIsNotAnError(t *testing.T) {
	dir := filepath.Join("..", "testdata", "dctx", "none", "context")
	f, err := LoadIgnoreFile(dir, "Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "" || len(f.Rules) != 0 {
		t.Errorf("LoadIgnoreFile() = %#v, want an empty result", f)
	}
}

func TestCheckContextDirRejectsAFile(t *testing.T) {
	// "context ls path/to/Dockerfile" is the natural mistake, because the image
	// commands do take file paths. Before this check it failed with
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

func TestDockerfileWarningOnlyFiresForATypo(t *testing.T) {
	// A Dockerfile need not exist for its ignore file to be used, and an ignore
	// file need not exist for a context to be listed. Only when neither is
	// there is the name almost certainly a typo.
	dir := t.TempDir()
	for _, name := range []string{"Dockerfile", "Ignoreonly.dockerignore"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name       string
		dockerfile string
		want       bool
	}{
		{"auto-detected, nothing to warn about", "", false},
		{"the Dockerfile is there", "Dockerfile", false},
		{"only the ignore file is there", "Ignoreonly", false},
		{"neither is there", "Dockerfle.typo", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dockerfileWarning(dir, tt.dockerfile, ".dockerignore")
			if (got != "") != tt.want {
				t.Errorf("dockerfileWarning(%q) = %q, want warning: %v", tt.dockerfile, got, tt.want)
			}
		})
	}
}
