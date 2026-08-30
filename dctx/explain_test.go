package dctx

import (
	"path/filepath"
	"testing"
)

func TestExplainReportsDecisiveRule(t *testing.T) {
	dir := filepath.Join("..", "testdata", "dctx", "reinclude", "context")

	exp, err := Explain(Options{Context: dir}, "node_modules/keep/index.js")
	if err != nil {
		t.Fatal(err)
	}
	if exp.Status != StatusIncluded {
		t.Errorf("status = %q, want included", exp.Status)
	}
	if len(exp.Rules) != 2 {
		t.Fatalf("matched %d rules, want 2", len(exp.Rules))
	}
	if exp.Rules[0].Rule != "node_modules" || exp.Rules[0].Decisive {
		t.Errorf("first rule = %+v, want node_modules and not decisive", exp.Rules[0])
	}
	last := exp.Rules[1]
	if last.Rule != "!node_modules/keep" || !last.Negated || !last.Decisive {
		t.Errorf("last rule = %+v, want the decisive negation", last)
	}
	if last.Line != 2 {
		t.Errorf("line = %d, want 2", last.Line)
	}
}

func TestExplainOnUnmatchedPath(t *testing.T) {
	dir := filepath.Join("..", "testdata", "dctx", "reinclude", "context")
	exp, err := Explain(Options{Context: dir}, "app.js")
	if err != nil {
		t.Fatal(err)
	}
	if exp.Status != StatusIncluded {
		t.Errorf("status = %q, want included", exp.Status)
	}
	if len(exp.Rules) != 0 {
		t.Errorf("rules = %+v, want none", exp.Rules)
	}
	if !exp.Exists {
		t.Error("app.js exists in the fixture")
	}
}

// Explaining a path that is not there is useful while editing .dockerignore,
// so it must work and say so.
func TestExplainHypotheticalPath(t *testing.T) {
	dir := filepath.Join("..", "testdata", "dctx", "reinclude", "context")
	exp, err := Explain(Options{Context: dir}, "node_modules/other/thing.js")
	if err != nil {
		t.Fatal(err)
	}
	if exp.Exists {
		t.Error("path should be reported as absent")
	}
	if exp.Status != StatusIgnored {
		t.Errorf("status = %q, want ignored", exp.Status)
	}
}

func TestExplainRejectsPathsOutsideTheContext(t *testing.T) {
	dir := filepath.Join("..", "testdata", "dctx", "reinclude", "context")
	for _, target := range []string{"..", "../elsewhere", "."} {
		if _, err := Explain(Options{Context: dir}, target); err == nil {
			t.Errorf("Explain(%q) succeeded, want an error", target)
		}
	}
}
