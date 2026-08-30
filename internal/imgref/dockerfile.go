package imgref

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

// ScanDockerfile finds every image reference in a Dockerfile.
//
// Parsing is delegated to BuildKit's own parser, so the set of instructions
// understood here is the set docker build understands. instructions.Parse
// tolerates a nil linter (Linter.Run checks for it), which is why no linting
// context is threaded through.
func ScanDockerfile(path string, data []byte) ([]Ref, error) {
	res, err := parser.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	stages, _, err := instructions.Parse(res.AST, nil)
	if err != nil {
		return nil, err
	}

	li := newLineIndex(data)
	// Stage names are declared as the file is read, and a later FROM may refer
	// to an earlier stage rather than to an image. Docker compares these
	// case-insensitively.
	declared := map[string]bool{}
	var refs []Ref

	for i, stage := range stages {
		if line, ok := firstLine(stage.Location); ok {
			refs = append(refs, classify(path, data, li, line, KindDockerfileFrom, stage.BaseName, stage.Name, declared))
		}
		for _, cmd := range stage.Commands {
			copyCmd, isCopy := cmd.(*instructions.CopyCommand)
			if !isCopy || copyCmd.From == "" {
				continue
			}
			line, ok := firstLine(copyCmd.Location())
			if !ok {
				continue
			}
			refs = append(refs, classify(path, data, li, line, KindDockerfileCopyFrom, copyCmd.From, "", declared))
		}
		// Register after processing, so `FROM x AS x` still reads as an image.
		if stage.Name != "" {
			declared[strings.ToLower(stage.Name)] = true
		}
		// A stage can also be referenced by its index.
		declared[strconv.Itoa(i)] = true
	}
	return refs, nil
}

func firstLine(ranges []parser.Range) (int, bool) {
	if len(ranges) == 0 {
		return 0, false
	}
	return ranges[0].Start.Line, true
}

// classify turns one raw reference into a Ref, deciding whether it names an
// image at all.
func classify(path string, data []byte, li lineIndex, line int, kind Kind, raw, stageName string, declared map[string]bool) Ref {
	if declared[strings.ToLower(raw)] {
		r := unresolved(path, line, kind, raw, "refers to an earlier build stage, not an image")
		r.Stage = stageName
		return r
	}
	if strings.ContainsAny(raw, "$") {
		r := unresolved(path, line, kind, raw, "depends on a build argument")
		r.Stage = stageName
		return r
	}
	if strings.EqualFold(raw, "scratch") {
		r := unresolved(path, line, kind, raw, "scratch is the empty base, not a registry image")
		r.Stage = stageName
		return r
	}

	ref := Ref{Path: path, Line: line, Kind: kind, Raw: raw, Stage: stageName}
	parsed, err := name.ParseReference(raw)
	if err != nil {
		ref.Note = err.Error()
		return ref
	}
	fill(&ref, parsed)
	if start, end, ok := li.offsetOf(data, line, raw); ok {
		ref.Start, ref.End = start, end
		ref.Resolved = true
	} else {
		ref.Note = "could not locate the reference on its line"
	}
	return ref
}

func fill(ref *Ref, parsed name.Reference) {
	ctx := parsed.Context()
	ref.Registry = ctx.RegistryStr()
	ref.Repository = ctx.RepositoryStr()
	switch v := parsed.(type) {
	case name.Tag:
		ref.Tag = v.TagStr()
	case name.Digest:
		ref.Digest = v.DigestStr()
		// "repo:tag@sha256:..." keeps its tag, which ParseReference drops.
		if i := strings.LastIndex(ref.Raw, "@"); i > 0 {
			if j := strings.LastIndex(ref.Raw[:i], ":"); j > strings.LastIndex(ref.Raw[:i], "/") {
				ref.Tag = ref.Raw[j+1 : i]
			}
		}
	}
}
