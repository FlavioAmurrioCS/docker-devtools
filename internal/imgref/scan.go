package imgref

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// composeName matches the Compose file names the docker CLI looks for, plus
// the profile/override variants people use in practice.
var composeName = regexp.MustCompile(`^(docker-)?compose(\.[^.]+)*\.ya?ml$`)

// FileKind reports how a path should be parsed, or "" when it is neither.
func FileKind(path string) Kind {
	base := filepath.Base(path)
	switch {
	case base == "Dockerfile" || base == "dockerfile" || base == "Containerfile":
		return KindDockerfileFrom
	case strings.HasPrefix(base, "Dockerfile."), strings.HasSuffix(base, ".Dockerfile"):
		return KindDockerfileFrom
	case composeName.MatchString(base):
		return KindComposeImage
	default:
		return ""
	}
}

// skipDir names directories never worth walking into.
var skipDir = map[string]bool{
	".git": true, "node_modules": true, "vendor": true,
	".venv": true, "venv": true, "__pycache__": true,
}

// Scan finds image references under each path. A path may be a file, which is
// parsed by its name, or a directory, which is walked.
//
// With no paths, the working directory is scanned.
func Scan(paths ...string) (*Result, error) {
	if len(paths) == 0 {
		paths = []string{"."}
	}
	res := &Result{Schema: SchemaVersion}
	seen := map[string]bool{}

	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			scanOne(res, p, seen)
			continue
		}
		err = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				res.Warnings = append(res.Warnings, errFile(path, err))
				return nil //nolint:nilerr // an unreadable entry is reported, not fatal
			}
			if d.IsDir() {
				if skipDir[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if FileKind(path) != "" {
				scanOne(res, path, seen)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	sort.SliceStable(res.Refs, func(i, j int) bool {
		if res.Refs[i].Path != res.Refs[j].Path {
			return res.Refs[i].Path < res.Refs[j].Path
		}
		return res.Refs[i].Line < res.Refs[j].Line
	})
	return res, nil
}

func scanOne(res *Result, path string, seen map[string]bool) {
	clean := filepath.Clean(path)
	if seen[clean] {
		return
	}
	seen[clean] = true

	kind := FileKind(clean)
	if kind == "" {
		res.Warnings = append(res.Warnings, clean+": not a Dockerfile or Compose file")
		return
	}
	data, err := os.ReadFile(clean)
	if err != nil {
		res.Warnings = append(res.Warnings, errFile(clean, err))
		return
	}

	var refs []Ref
	if kind == KindComposeImage {
		refs, err = ScanCompose(clean, data)
	} else {
		refs, err = ScanDockerfile(clean, data)
	}
	if err != nil {
		res.Warnings = append(res.Warnings, errFile(clean, err))
		return
	}
	res.Refs = append(res.Refs, refs...)
}
