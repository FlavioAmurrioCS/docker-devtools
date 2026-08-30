package imgref

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

// render is a stable, diffable view of a scan. Byte offsets are shown because
// a rewrite depends on them and a silent drift would be invisible otherwise.
func render(res *Result) string {
	var b strings.Builder
	for _, r := range res.Refs {
		fmt.Fprintf(&b, "%s:%d %s %q", r.Path, r.Line, r.Kind, r.Raw)
		if r.Stage != "" {
			fmt.Fprintf(&b, " stage=%s", r.Stage)
		}
		if r.Resolved {
			fmt.Fprintf(&b, " -> registry=%s repo=%s", r.Registry, r.Repository)
			if r.Tag != "" {
				fmt.Fprintf(&b, " tag=%s", r.Tag)
			}
			if r.Digest != "" {
				fmt.Fprintf(&b, " digest=%s", r.Digest)
			}
			fmt.Fprintf(&b, " bytes=%d..%d", r.Start, r.End)
		} else {
			fmt.Fprintf(&b, " UNRESOLVED (%s)", r.Note)
		}
		b.WriteString("\n")
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(&b, "warning: %s\n", w)
	}
	return b.String()
}

func fixtures(t *testing.T) []string {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join("..", "..", "testdata", "imageref", "*"))
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	return dirs
}

func TestScanGolden(t *testing.T) {
	for _, dir := range fixtures(t) {
		name := filepath.Base(dir)
		t.Run(name, func(t *testing.T) {
			res, err := Scan(dir)
			if err != nil {
				t.Fatal(err)
			}
			// Paths embed the fixture directory; strip it so goldens are stable.
			for i := range res.Refs {
				res.Refs[i].Path = filepath.Base(res.Refs[i].Path)
			}
			got := render(res)

			golden := filepath.Join(dir, "expected.txt")
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
				t.Errorf("golden mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
			}
		})
	}
}

// TestByteRangesLandOnTheReference is the safety net under in-place rewriting:
// every resolved reference's byte range must contain exactly its own text.
func TestByteRangesLandOnTheReference(t *testing.T) {
	for _, dir := range fixtures(t) {
		res, err := Scan(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range res.Refs {
			if !r.Resolved {
				continue
			}
			data, err := os.ReadFile(r.Path)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(data[r.Start:r.End]); got != r.Raw {
				t.Errorf("%s:%d byte range %d..%d holds %q, want %q",
					r.Path, r.Line, r.Start, r.End, got, r.Raw)
			}
		}
	}
}

func TestStageReferencesAreNotImages(t *testing.T) {
	res, err := Scan(filepath.Join("..", "..", "testdata", "imageref", "multistage"))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res.Refs {
		switch r.Raw {
		case "builder", "0":
			if r.Resolved {
				t.Errorf("%q resolved as an image; it names a build stage", r.Raw)
			}
		case "golang:1.26-alpine", "alpine:3.22", "busybox:1.37":
			if !r.Resolved {
				t.Errorf("%q should resolve as an image, got %q", r.Raw, r.Note)
			}
		}
	}
}

func TestFileKind(t *testing.T) {
	cases := map[string]Kind{
		"Dockerfile":        KindDockerfileFrom,
		"dockerfile":        KindDockerfileFrom,
		"Containerfile":     KindDockerfileFrom,
		"Dockerfile.pinned": KindDockerfileFrom,
		"api.Dockerfile":    KindDockerfileFrom,
		// Both spellings take both affixes, case-insensitively.
		"dockerfile.dev":      KindDockerfileFrom,
		"Containerfile.dev":   KindDockerfileFrom,
		"api.Containerfile":   KindDockerfileFrom,
		"containerfile":       KindDockerfileFrom,
		"compose.yaml":        KindComposeImage,
		"compose.yml":         KindComposeImage,
		"docker-compose.yaml": KindComposeImage,
		"compose.prod.yaml":   KindComposeImage,
		"README.md":           "",
		"values.yaml":         "",
		// An ignore file is named after its Dockerfile and sits beside it, so
		// it matches the "Dockerfile." prefix. Parsing it as instructions
		// reports "unknown instruction" for a valid file.
		".dockerignore":                 "",
		"Dockerfile.dockerignore":       "",
		"Dockerfile.dev.dockerignore":   "",
		"api.Dockerfile.dockerignore":   "",
		"build.Dockerfile.dockerignore": "",
	}
	for in, want := range cases {
		if got := FileKind(in); got != want {
			t.Errorf("FileKind(%q) = %q, want %q", in, got, want)
		}
	}
}
