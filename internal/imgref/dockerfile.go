package imgref

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/moby/buildkit/frontend/dockerfile/shell"
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
	stages, metaArgs, err := instructions.Parse(res.AST, nil)
	if err != nil {
		return nil, err
	}

	li := newLineIndex(data)
	args := newMetaArgs(res.EscapeToken, metaArgs)
	// Stage names are declared as the file is read, and a later FROM may refer
	// to an earlier stage rather than to an image. Docker compares these
	// case-insensitively.
	declared := map[string]bool{}
	var refs []Ref

	// One ARG can back several FROMs, and each would otherwise contribute the
	// same edit over the same bytes. rewrite.Apply is right to reject that, so
	// emit each ARG-anchored reference once.
	emittedArg := map[int]bool{}

	for i, stage := range stages {
		if line, ok := firstLine(stage.Location); ok {
			if argRef, from, ok := args.resolve(path, data, li, line, stage); ok {
				if !emittedArg[argRef.Start] {
					emittedArg[argRef.Start] = true
					refs = append(refs, argRef)
				}
				refs = append(refs, from)
			} else {
				refs = append(refs, classify(path, data, li, line, KindDockerfileFrom, stage.BaseName, stage.Name, declared))
			}
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

// metaArgs holds the ARG defaults declared before the first FROM, which are the
// only ones a FROM may use, along with where each one sits in the file.
//
// Values are expanded in declaration order against the ones already defined,
// mirroring buildkit's buildMetaArgs (dockerfile2llb/convert.go). The lexer is
// buildkit's own, so quoting and expansion behave exactly as docker build's do:
// FROM "${BASE}" arrives here with its quotes still attached and comes back
// without them.
type metaArgs struct {
	lex *shell.Lex
	env []string
	// line records the source line of the ARG that defined each key, so a
	// reference resolved through it can be anchored where an edit would land.
	line map[string]int
}

func newMetaArgs(escape rune, cmds []instructions.ArgCommand) *metaArgs {
	m := &metaArgs{lex: shell.NewLex(escape), line: map[string]int{}}
	for _, cmd := range cmds {
		argLine, ok := firstLine(cmd.Location())
		for _, kp := range cmd.Args {
			if kp.Value == nil {
				// "ARG BASE" with no default: nothing to resolve against, and
				// docker build would demand --build-arg for it.
				continue
			}
			value := *kp.Value
			if expanded, err := m.lex.ProcessWordWithMatches(value, shell.EnvsFromSlice(m.env)); err == nil {
				value = expanded.Result
			}
			m.env = append(m.env, kp.Key+"="+value)
			if ok {
				m.line[kp.Key] = argLine
			}
		}
	}
	return m
}

// resolve expands a stage's base name through the meta ARGs and, when the
// result is an image reference that is written out verbatim on an ARG line,
// returns that ARG as a resolved reference plus the FROM that pointed at it.
//
// The verbatim requirement is the safety rule, not a convenience: a reference
// must own the bytes it would rewrite. "ARG BASE=debian:13-slim" owns them.
// "ARG VERSION=1.2.3" with "FROM debian:${VERSION}" does not -- the expansion
// is debian:1.2.3, which appears on no line -- so it stays unresolved rather
// than tempting an update to splice a bare tag fragment.
func (m *metaArgs) resolve(path string, data []byte, li lineIndex, line int, stage instructions.Stage) (argRef, fromRef Ref, ok bool) {
	if !strings.Contains(stage.BaseName, "$") {
		return Ref{}, Ref{}, false
	}
	expanded, err := m.lex.ProcessWordWithMatches(stage.BaseName, shell.EnvsFromSlice(m.env))
	if err != nil || expanded.Result == "" {
		return Ref{}, Ref{}, false
	}

	for key := range expanded.Matched {
		argLine, known := m.line[key]
		if !known {
			continue
		}
		start, end, found := li.offsetOf(data, argLine, expanded.Result)
		if !found {
			continue
		}
		ref := Ref{
			Path: path, Line: argLine, Kind: KindDockerfileArg,
			Raw: expanded.Result, Start: start, End: end,
		}
		parsed, err := name.ParseReference(expanded.Result)
		if err != nil {
			continue
		}
		fill(&ref, parsed)
		ref.Resolved = true

		from := unresolved(path, line, KindDockerfileFrom, stage.BaseName,
			fmt.Sprintf("resolved from ARG %s on line %d", key, argLine))
		from.Stage = stage.Name
		return ref, from, true
	}
	return Ref{}, Ref{}, false
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
