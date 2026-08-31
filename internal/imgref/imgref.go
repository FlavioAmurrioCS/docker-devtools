// Package imgref finds container image references in Dockerfiles and Compose
// files, recording exactly where each one sits in the source.
//
// Positions are byte ranges covering the reference itself, not the whole line,
// so an update can splice a replacement in without re-encoding the file. That
// is what keeps comments, quoting and formatting intact.
package imgref

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SchemaVersion is the version of the JSON document a scan marshals to.
// Consumers should refuse documents with an unknown value.
const SchemaVersion = 1

// Kind is where a reference was found.
type Kind string

const (
	// KindDockerfileFrom is a FROM instruction's base image.
	KindDockerfileFrom Kind = "dockerfile-from"
	// KindDockerfileCopyFrom is a COPY --from=<image> source.
	KindDockerfileCopyFrom Kind = "dockerfile-copy-from"
	// KindDockerfileArg is an ARG default that a FROM resolves to. The
	// reference is anchored on the ARG line, because that is the only text an
	// update can rewrite: the FROM holds a variable, not an image.
	KindDockerfileArg Kind = "dockerfile-arg"
	// KindComposeImage is a Compose service's image: value.
	KindComposeImage Kind = "compose-image"
)

// Ref is one image reference and its position in a file.
type Ref struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Kind Kind   `json:"kind"`
	// Raw is the reference exactly as written.
	Raw string `json:"raw"`

	// Registry, Repository, Tag and Digest are the parsed parts, present only
	// when Resolved is true.
	Registry   string `json:"registry,omitempty"`
	Repository string `json:"repository,omitempty"`
	Tag        string `json:"tag,omitempty"`
	Digest     string `json:"digest,omitempty"`

	// Stage is the name a FROM instruction binds with AS, when it has one.
	Stage string `json:"stage,omitempty"`

	// Resolved reports whether Raw is a literal reference. Build arguments
	// make it false: the value depends on args we were not given, so it is
	// reported rather than guessed at.
	Resolved bool `json:"resolved"`
	// Note explains an unresolved reference.
	Note string `json:"note,omitempty"`

	// Start and End bound Raw within the file, for in-place rewriting.
	Start int `json:"-"`
	End   int `json:"-"`
}

// Result is the output of scanning a set of files.
type Result struct {
	Schema int   `json:"schema"`
	Refs   []Ref `json:"refs"`
	// Warnings records files that could not be parsed, rather than dropping
	// them silently.
	Warnings []string `json:"warnings,omitempty"`
}

// MarshalJSON is defined so an empty scan emits [] rather than null.
func (r Result) MarshalJSON() ([]byte, error) {
	type alias Result
	if r.Refs == nil {
		r.Refs = []Ref{}
	}
	return json.Marshal(alias(r))
}

// lineIndex maps 1-based line numbers to their starting byte offset.
type lineIndex []int

func newLineIndex(data []byte) lineIndex {
	// Index 0 is unused so that lookups can be 1-based like the parsers are.
	idx := lineIndex{0, 0}
	for i, b := range data {
		if b == '\n' {
			idx = append(idx, i+1)
		}
	}
	return idx
}

// offsetOf returns the byte range of the first occurrence of token on the given
// 1-based line. It reports false when the line or the token is not there,
// which keeps a rewrite from ever guessing at a position.
func (li lineIndex) offsetOf(data []byte, line int, token string) (start, end int, ok bool) {
	if line <= 0 || line >= len(li) {
		return 0, 0, false
	}
	lineStart := li[line]
	lineEnd := len(data)
	if line+1 < len(li) {
		lineEnd = li[line+1]
	}
	rel := strings.Index(string(data[lineStart:lineEnd]), token)
	if rel < 0 {
		return 0, 0, false
	}
	return lineStart + rel, lineStart + rel + len(token), true
}

func unresolved(path string, line int, kind Kind, raw, note string) Ref {
	return Ref{
		Path: path, Line: line, Kind: kind, Raw: raw,
		Resolved: false, Note: note,
	}
}

func errFile(path string, err error) string {
	return fmt.Sprintf("%s: %v", path, err)
}
