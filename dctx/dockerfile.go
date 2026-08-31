package dctx

import (
	"bytes"
	"context"
	"fmt"
	gofs "io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/tonistiigi/fsutil"
)

// ContextPaths reports the paths a build reads from the build context, and
// whether it reads the context whole.
//
// BuildKit sends only the paths a Dockerfile actually references. Every COPY
// and ADD source is collected into ctxPaths and handed to the context local as
// llb.FollowPaths (dockerfile2llb/convert.go, filterPaths), which is why
// "ADD taplo.toml /tmp/" transfers one file rather than the whole tree.
//
// whole is true when any source normalizes to "/", which switches the filter
// off entirely (normalizeContextPaths returns nil). "COPY . /" does that, and
// it is why a listing of the full context is right for some Dockerfiles and
// badly wrong for others.
//
// target names the stage to build, as docker build --target does. Empty means
// the last stage.
func ContextPaths(data []byte, target string) (paths []string, whole bool, err error) {
	res, err := parser.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, false, err
	}
	stages, _, err := instructions.Parse(res.AST, nil)
	if err != nil {
		return nil, false, err
	}
	if len(stages) == 0 {
		return nil, false, nil
	}

	start, err := targetStage(stages, target)
	if err != nil {
		return nil, false, err
	}

	set := map[string]struct{}{}
	for _, i := range reachable(stages, start) {
		for _, src := range stageContextPaths(stages[i]) {
			p := path.Join("/", filepath.ToSlash(src))
			if p == "/" {
				// The whole context is pulled, so there is nothing to filter.
				return nil, true, nil
			}
			set[strings.TrimPrefix(p, "/")] = struct{}{}
		}
	}

	paths = make([]string, 0, len(set))
	for p := range set {
		paths = append(paths, p)
	}
	return paths, false, nil
}

// stageContextPaths returns the sources one stage reads from the context.
//
// The rules mirror the dispatch switch in dockerfile2llb/convert.go: ADD
// contributes its sources except remote URLs, COPY contributes its sources only
// when it has no --from, and nothing else touches the context. RUN marks the
// stage's own filesystem used, not the context's.
func stageContextPaths(stage instructions.Stage) []string {
	var out []string
	for _, cmd := range stage.Commands {
		switch c := cmd.(type) {
		case *instructions.AddCommand:
			for _, src := range c.SourcePaths {
				if !strings.HasPrefix(src, "http://") && !strings.HasPrefix(src, "https://") {
					out = append(out, src)
				}
			}
		case *instructions.CopyCommand:
			if c.From == "" {
				out = append(out, c.SourcePaths...)
			}
		}
	}
	return out
}

// targetStage returns the index of the stage to build.
//
// No target means the last stage, which is what resolveTarget does with
// lastTarget() (dockerfile2llb/convert.go). A named target is matched
// case-insensitively, as findStateByName lowercases both sides, and the *last*
// stage with that name wins: buildkit keeps stages in a map keyed by name, so a
// repeated name overwrites the earlier entry. Verified against docker, which
// builds the second of two stages both named "dup".
func targetStage(stages []instructions.Stage, target string) (int, error) {
	if target == "" {
		return len(stages) - 1, nil
	}
	found := -1
	for i, s := range stages {
		if s.Name != "" && strings.EqualFold(s.Name, target) {
			found = i
		}
	}
	if found < 0 {
		return 0, &unknownTargetError{target: target}
	}
	return found, nil
}

type unknownTargetError struct{ target string }

func (e *unknownTargetError) Error() string {
	return "target stage " + strconv.Quote(e.target) + " is not defined in the Dockerfile"
}

// reachable returns the stages a build of start actually evaluates.
//
// A stage depends on the stage its FROM names, and on every stage a
// COPY --from names. Stages outside that set are never built, so their COPY
// sources are never read from the context -- which is the difference between
// reporting a vendor directory as transferred and not.
func reachable(stages []instructions.Stage, start int) []int {
	byName := map[string]int{}
	for i, s := range stages {
		if s.Name != "" {
			byName[strings.ToLower(s.Name)] = i
		}
		// A stage can also be named by its index.
		byName[strconv.Itoa(i)] = i
	}
	// Only earlier stages are visible, the same rule dispatch enforces with
	// "cannot copy from stage %q, it needs to be defined before current stage".
	resolve := func(name string, before int) (int, bool) {
		i, ok := byName[strings.ToLower(name)]
		return i, ok && i < before
	}

	seen := map[int]bool{}
	var visit func(int)
	visit = func(i int) {
		if i < 0 || i >= len(stages) || seen[i] {
			return
		}
		seen[i] = true
		if base, ok := resolve(stages[i].BaseName, i); ok {
			visit(base)
		}
		for _, cmd := range stages[i].Commands {
			if c, isCopy := cmd.(*instructions.CopyCommand); isCopy && c.From != "" {
				if from, ok := resolve(c.From, i); ok {
					visit(from)
				}
			}
		}
	}
	visit(start)

	out := make([]int, 0, len(seen))
	for i := range stages {
		if seen[i] {
			out = append(out, i)
		}
	}
	return out
}

// transferredPaths returns the set of paths a build reads from the context, or
// nil when it reads the context whole.
//
// The filtering is fsutil's own: NewFilterFS with FollowPaths and
// ExcludePatterns is the call BuildKit makes through MainContext, so symlink
// resolution, wildcard sources and directory sources covering their subtrees
// all behave exactly as they do in a build, with no matching logic here.
func transferredPaths(
	ctx context.Context,
	root fsutil.FS,
	contextDir string,
	dockerfile DockerfileRef,
	ignoreFile *IgnoreFile,
	opt Options,
) (map[string]struct{}, error) {
	if opt.WholeContext {
		return nil, nil
	}
	data, err := os.ReadFile(dockerfile.Path) //nolint:gosec // the Dockerfile the caller named
	if err != nil {
		return nil, err
	}
	paths, whole, err := ContextPaths(data, opt.Target)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dockerfile.Display, err)
	}
	if whole {
		return nil, nil
	}
	// No COPY or ADD reads the context at all, so nothing is sent.
	if len(paths) == 0 {
		return map[string]struct{}{}, nil
	}

	filtered, err := fsutil.NewFilterFS(root, &fsutil.FilterOpt{
		FollowPaths:     paths,
		ExcludePatterns: ignoreFile.Patterns(),
	})
	if err != nil {
		return nil, err
	}

	out := map[string]struct{}{}
	err = filtered.Walk(ctx, "/", func(p string, _ gofs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		out[filepath.ToSlash(p)] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", contextDir, err)
	}
	return out, nil
}
