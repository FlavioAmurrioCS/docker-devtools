package imgupdate

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlavioAmurrioCS/docker-devtools/internal/imgref"
	"github.com/google/go-containerregistry/pkg/name"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	v1random "github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	localregistry "github.com/FlavioAmurrioCS/docker-devtools/internal/registry"
)

// startRegistry runs go-containerregistry's in-process registry and pushes a
// distinct image for each tag. Tests therefore exercise the real registry
// protocol with no network and no fixtures to keep in sync.
func startRegistry(t *testing.T, repo string, tags ...string) string {
	t.Helper()
	// Silence the registry's request log; a failing test says what it needs.
	quiet := log.New(io.Discard, "", 0)
	srv := httptest.NewServer(ggcrregistry.New(ggcrregistry.Logger(quiet)))
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	for i, tag := range tags {
		img, err := v1random.Image(int64(64+i), 1)
		if err != nil {
			t.Fatal(err)
		}
		ref, err := name.ParseReference(fmt.Sprintf("%s/%s:%s", u.Host, repo, tag))
		if err != nil {
			t.Fatal(err)
		}
		if err := remote.Write(ref, img); err != nil {
			t.Fatal(err)
		}
	}
	return u.Host
}

// writeDockerfile lays down a Dockerfile and returns its path plus the scanned
// references, so the test covers scan, plan and apply together.
func writeDockerfile(t *testing.T, body string) (string, []imgref.Ref) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := imgref.Scan(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, res.Refs
}

func TestUpdateBumpsTagInPlace(t *testing.T) {
	host := startRegistry(t, "library/app", "1.0", "1.1", "2.0")
	body := fmt.Sprintf("# a comment worth keeping\nFROM %s/library/app:1.0 AS base\nRUN true   # and this one\n", host)
	path, refs := writeDockerfile(t, body)

	report, err := Plan(context.Background(), localregistry.New(), refs, Options{Policy: PolicySamePattern})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Changes) != 1 {
		t.Fatalf("planned %d changes, want 1: %+v (warnings %v)", len(report.Changes), report.Changes, report.Warnings)
	}
	if !strings.HasSuffix(report.Changes[0].New, ":1.1") {
		t.Errorf("new reference is %q, want it on tag 1.1", report.Changes[0].New)
	}

	if _, err := Apply(report); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(body, "/library/app:1.0", "/library/app:1.1", 1)
	if string(got) != want {
		t.Errorf("file is\n%q\nwant\n%q", got, want)
	}
}

func TestUpdatePinsDigest(t *testing.T) {
	host := startRegistry(t, "library/app", "1.0")
	body := fmt.Sprintf("FROM %s/library/app:1.0\n", host)
	path, refs := writeDockerfile(t, body)

	report, err := Plan(context.Background(), localregistry.New(), refs, Options{PinDigest: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Changes) != 1 {
		t.Fatalf("planned %d changes, want 1 (warnings %v)", len(report.Changes), report.Warnings)
	}
	if !strings.Contains(report.Changes[0].New, "@sha256:") {
		t.Errorf("new reference %q should carry a digest", report.Changes[0].New)
	}
	if !strings.Contains(report.Changes[0].New, ":1.0@") {
		t.Errorf("new reference %q should keep its tag alongside the digest", report.Changes[0].New)
	}

	if _, err := Apply(report); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "@sha256:") {
		t.Errorf("file was not pinned: %q", got)
	}
}

// A second run must change nothing, which is what pre-commit requires.
func TestUpdateIsIdempotent(t *testing.T) {
	host := startRegistry(t, "library/app", "1.0", "1.1")
	body := fmt.Sprintf("FROM %s/library/app:1.0\n", host)
	path, refs := writeDockerfile(t, body)
	opts := Options{Policy: PolicySamePattern, PinDigest: true}

	report, err := Plan(context.Background(), localregistry.New(), refs, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(report); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	res, err := imgref.Scan(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Plan(context.Background(), localregistry.New(), res.Refs, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Changes) != 0 {
		t.Errorf("second pass planned %d changes, want none: %+v", len(second.Changes), second.Changes)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(first) {
		t.Error("second pass changed the file")
	}
}

// Unresolved references must be counted and left alone rather than rewritten on
// a guess. An ARG default is not one of them any more -- see the test below --
// so this uses the kinds that genuinely cannot be resolved: an unset ARG, a
// fragment that no line spells out, and scratch.
func TestUpdateSkipsUnresolvedReferences(t *testing.T) {
	body := "ARG UNSET\nARG VERSION=3.22\nFROM $UNSET\nFROM alpine:${VERSION}\nFROM scratch\n"
	_, refs := writeDockerfile(t, body)

	report, err := Plan(context.Background(), localregistry.New(), refs, Options{PinDigest: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Changes) != 0 {
		t.Errorf("planned %d changes for unresolved references", len(report.Changes))
	}
	if report.Skipped != 3 {
		t.Errorf("skipped %d, want 3", report.Skipped)
	}
}

// An ARG default that a FROM resolves to is the base image, and the ARG line is
// the only place an update can write it. Renovate treats it the same way.
func TestUpdateBumpsAnArgDefault(t *testing.T) {
	host := startRegistry(t, "library/app", "1.0", "1.1")
	body := fmt.Sprintf("ARG BASE=%s/library/app:1.0\nFROM ${BASE} AS one\nFROM ${BASE} AS two\n", host)
	path, refs := writeDockerfile(t, body)

	report, err := Plan(context.Background(), localregistry.New(), refs, Options{Policy: PolicySamePattern})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Changes) != 1 {
		t.Fatalf("planned %d changes, want 1: two FROMs share one ARG, and two edits "+
			"over the same bytes would be rejected", len(report.Changes))
	}
	if report.Changes[0].Line != 1 {
		t.Errorf("change reported on line %d, want 1, the ARG line it writes",
			report.Changes[0].Line)
	}
	if _, err := Apply(report); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("ARG BASE=%s/library/app:1.1\nFROM ${BASE} AS one\nFROM ${BASE} AS two\n", host)
	if string(after) != want {
		t.Errorf("after update:\n%s\nwant:\n%s", after, want)
	}
}

func TestPlanRejectsUnknownPolicy(t *testing.T) {
	_, err := Plan(context.Background(), localregistry.New(), nil, Options{Policy: "yolo"})
	if err == nil {
		t.Fatal("an unknown policy should be refused")
	}
}
