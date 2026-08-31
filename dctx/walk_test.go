package dctx

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

// fixtureCase is the optional case.json beside a fixture context.
type fixtureCase struct {
	// Dockerfile is relative to the fixture directory, not to the context, so
	// that the same string can be handed to our binary and to docker build.
	// scripts/conformance.sh passes it to both.
	Dockerfile string `json:"dockerfile"`
	// Target pins a --target, which changes which stages are reached and so
	// which COPY sources are read from the context.
	Target string `json:"target"`
}

// dockerfilePath returns the -f value for a fixture, which is a path resolved
// from the working directory, exactly as docker build resolves it.
func (c fixtureCase) dockerfilePath(dir string) string {
	if c.Dockerfile == "" {
		return ""
	}
	return filepath.Join(dir, c.Dockerfile)
}

func fixtures(t *testing.T) []string {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join("..", "testdata", "dctx", "*"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range dirs {
		if fi, err := os.Stat(filepath.Join(d, "context")); err == nil && fi.IsDir() {
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		t.Fatal("no fixtures found")
	}
	return out
}

func loadCase(t *testing.T, dir string) fixtureCase {
	t.Helper()
	var c fixtureCase
	data, err := os.ReadFile(filepath.Join(dir, "case.json"))
	if os.IsNotExist(err) {
		return c
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

// render turns a result into a stable, diffable form. The context path is
// absolute and machine-specific, so it is deliberately left out.
func render(res *Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "dockerfile: %s\n", res.Dockerfile)
	fmt.Fprintf(&b, "ignorefile: %s\n", res.Ignorefile)
	fmt.Fprintf(&b, "included:   %d files, %d bytes\n",
		res.Summary.Included.Files, res.Summary.Included.Bytes)
	if res.Summary.Ignored != nil {
		fmt.Fprintf(&b, "ignored:    %d files, %d bytes\n",
			res.Summary.Ignored.Files, res.Summary.Ignored.Bytes)
	} else {
		b.WriteString("ignored:    not counted\n")
	}
	if t := res.Summary.Transferred; t != nil {
		fmt.Fprintf(&b, "sent:       %d files, %d bytes\n", t.Files, t.Bytes)
	} else {
		b.WriteString("sent:       the whole context\n")
	}
	b.WriteString("--\n")
	for _, e := range res.Entries {
		mark := "+"
		if e.Status == StatusIgnored {
			mark = "-"
		}
		if e.Materialized {
			mark = "M"
		}
		// A path the ignore rules permit but no COPY reads is not sent.
		if e.Status == StatusIncluded && !e.Transferred {
			mark = "."
		}
		fmt.Fprintf(&b, "%s %s", mark, e.Path)
		if e.Rule != "" {
			fmt.Fprintf(&b, "  <- %s:%d %s", res.Ignorefile, e.RuleLine, e.Rule)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func TestWalkGolden(t *testing.T) {
	for _, dir := range fixtures(t) {
		name := filepath.Base(dir)
		c := loadCase(t, dir)
		for _, mode := range []struct {
			name string
			mode Mode
		}{
			{"included", ModeIncluded},
			{"ignored", ModeIgnored},
			{"all", ModeAll},
		} {
			t.Run(name+"/"+mode.name, func(t *testing.T) {
				res, err := Walk(context.Background(), Options{
					Context:    filepath.Join(dir, "context"),
					Dockerfile: c.dockerfilePath(dir),
					Target:     c.Target,
					Mode:       mode.mode,
				})
				if err != nil {
					t.Fatal(err)
				}
				got := render(res)
				golden := filepath.Join(dir, mode.name+".golden")
				if *update {
					if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(golden)
				if err != nil {
					t.Fatalf("%v (run: go test ./... -update)", err)
				}
				if got != string(want) {
					t.Errorf("golden mismatch for %s\n--- want ---\n%s\n--- got ---\n%s",
						golden, want, got)
				}
			})
		}
	}
}

// TestDeepDoubleStarIsIgnored pins the bug that motivated this project.
//
// dolmen-go/docker-list-context calls the deprecated patternmatcher.Matches,
// which only checks one ancestor at the pattern's own depth, so it leaks
// a/b/__pycache__/x/y.pyc into the context under a "**/__pycache__" rule.
func TestDeepDoubleStarIsIgnored(t *testing.T) {
	res, err := Walk(context.Background(), Options{
		Context: filepath.Join("..", "testdata", "dctx", "pycache", "context"),
		Mode:    ModeIncluded,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range res.Entries {
		if strings.Contains(e.Path, "__pycache__") {
			t.Errorf("%s reached the build context but **/__pycache__ should ignore it", e.Path)
		}
	}
}

// TestMaterializedDirectory pins the behaviour the Docker conformance run
// found: an ignored directory is still sent when a negation re-includes
// something inside it, because it has to exist to hold it.
func TestMaterializedDirectory(t *testing.T) {
	res, err := Walk(context.Background(), Options{
		Context: filepath.Join("..", "testdata", "dctx", "reinclude", "context"),
		Mode:    ModeAll,
	})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range res.Entries {
		if e.Path != "node_modules" {
			continue
		}
		found = true
		if e.Status != StatusIncluded {
			t.Errorf("node_modules status = %q, want %q", e.Status, StatusIncluded)
		}
		if !e.Materialized {
			t.Error("node_modules should be marked materialized")
		}
		if e.Rule != "node_modules" {
			t.Errorf("node_modules rule = %q, want the rule that matched it", e.Rule)
		}
	}
	if !found {
		t.Error("node_modules is missing; Docker sends it to hold node_modules/keep")
	}
}

// TestIgnoredTotalsOnlyWhenCounted guards the documented schema contract:
// the default mode skips ignored subtrees, so it must report null rather than
// an undercount.
func TestIgnoredTotalsOnlyWhenCounted(t *testing.T) {
	dir := filepath.Join("..", "testdata", "dctx", "trailing", "context")

	included, err := Walk(context.Background(), Options{Context: dir, Mode: ModeIncluded})
	if err != nil {
		t.Fatal(err)
	}
	if included.Summary.Ignored != nil {
		t.Error("ModeIncluded skips ignored subtrees, so ignored totals must be nil")
	}

	all, err := Walk(context.Background(), Options{Context: dir, Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	if all.Summary.Ignored == nil {
		t.Fatal("ModeAll must report ignored totals")
	}
	if all.Summary.Ignored.Files != 2 {
		t.Errorf("ignored files = %d, want 2", all.Summary.Ignored.Files)
	}
}
