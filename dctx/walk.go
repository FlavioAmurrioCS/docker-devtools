package dctx

import (
	"context"
	"fmt"
	gofs "io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/moby/patternmatcher"
	"github.com/tonistiigi/fsutil"
)

// SchemaVersion is the version of the JSON document Result marshals to.
// Consumers should refuse documents with an unknown value.
const SchemaVersion = 1

// Status is whether a path reaches the daemon.
type Status string

const (
	// StatusIncluded marks a path Docker sends to the daemon.
	StatusIncluded Status = "included"
	// StatusIgnored marks a path the ignore file excluded.
	StatusIgnored Status = "ignored"
)

// Mode selects which entries a walk reports.
type Mode int

const (
	// ModeIncluded reports only what Docker sends. Ignored directories are
	// skipped wholesale when no rule could re-include anything inside them, so
	// ignored totals are not available in this mode.
	ModeIncluded Mode = iota
	// ModeIgnored reports only what the ignore file excluded.
	ModeIgnored
	// ModeAll reports every path with its status.
	ModeAll
)

// Options configures a walk.
type Options struct {
	// Context is the build context directory.
	Context string
	// Dockerfile is the -f value, used to derive "<name>.dockerignore". Empty
	// means auto-detect.
	Dockerfile string
	// Mode selects which entries are reported.
	Mode Mode
}

// Entry is one path in the build context.
type Entry struct {
	Path   string `json:"path"`
	Status Status `json:"status"`
	Size   int64  `json:"size"`
	Dir    bool   `json:"dir,omitempty"`
	// Materialized marks a directory that an ignore rule matched but that
	// Docker still sends, because a negated rule re-included something inside
	// it and the directory has to exist to hold it.
	Materialized bool `json:"materialized,omitempty"`
	// Rule, RuleLine and Negated describe the ignore-file line that decided
	// this path, when one matched.
	Rule     string `json:"rule,omitempty"`
	RuleLine int    `json:"rule_line,omitempty"`
	Negated  bool   `json:"negated,omitempty"`
}

// Counts is a file and byte tally. Directories are counted in neither.
type Counts struct {
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
}

// Summary totals both sides of the walk. Ignored is nil when the walk skipped
// ignored subtrees and so cannot report a complete total.
type Summary struct {
	Included Counts  `json:"included"`
	Ignored  *Counts `json:"ignored"`
}

// Result is the full output of a walk.
type Result struct {
	Schema     int      `json:"schema"`
	Context    string   `json:"context"`
	Dockerfile string   `json:"dockerfile"`
	Ignorefile string   `json:"ignorefile"`
	Summary    Summary  `json:"summary"`
	Entries    []Entry  `json:"entries"`
	Warnings   []string `json:"warnings,omitempty"`
}

// dirFrame is an ancestor directory of the path currently being visited.
//
// A directory's own fate cannot be settled until its subtree has been walked:
// an ignored directory still reaches the daemon if a negated rule re-included
// anything inside it. So frames are finalized on pop, not on visit.
type dirFrame struct {
	prefix    string
	matchInfo patternmatcher.MatchInfo
	entry     Entry
	ignored   bool
	// hasIncluded records whether anything inside this directory is sent.
	hasIncluded bool
}

// Walk lists the build context at opt.Context.
//
// Traversal and path normalization come from fsutil — the package BuildKit
// uses to send a build context — and every include/exclude decision comes from
// patternmatcher.MatchesUsingParentResults, threading each directory's match
// state to its children exactly as fsutil's filterFS does.
func Walk(ctx context.Context, opt Options) (*Result, error) {
	contextDir, err := filepath.Abs(opt.Context)
	if err != nil {
		return nil, err
	}
	if err := CheckContextDir(opt.Context); err != nil {
		return nil, err
	}

	dockerfile, err := ResolveDockerfile(opt.Context, opt.Dockerfile)
	if err != nil {
		return nil, err
	}
	ignoreFile, err := LoadIgnoreFile(contextDir, dockerfile)
	if err != nil {
		return nil, err
	}
	m, err := newMatcher(ignoreFile)
	if err != nil {
		return nil, err
	}

	root, err := fsutil.NewFS(contextDir)
	if err != nil {
		return nil, err
	}

	w := &walker{
		matcher: m,
		mode:    opt.Mode,
		res: &Result{
			Schema:     SchemaVersion,
			Context:    contextDir,
			Dockerfile: dockerfile.Display,
			Ignorefile: ignoreFile.Name,
			Entries:    []Entry{},
		},
	}
	if opt.Mode != ModeIncluded {
		w.res.Summary.Ignored = &Counts{}
	}
	// fsutil's own optimization: when nothing can be re-included, an ignored
	// directory's whole subtree is ignored and need not be visited. The other
	// modes descend anyway, because their totals require it.
	w.canSkip = opt.Mode == ModeIncluded && !m.hasExclusions()

	if err := root.Walk(ctx, "/", w.visit); err != nil {
		return nil, err
	}
	w.unwind(0)

	sort.Slice(w.res.Entries, func(i, j int) bool {
		return w.res.Entries[i].Path < w.res.Entries[j].Path
	})
	return w.res, nil
}

type walker struct {
	matcher *matcher
	mode    Mode
	canSkip bool
	stack   []dirFrame
	res     *Result
}

func (w *walker) visit(path string, d gofs.DirEntry, err error) error {
	if err != nil {
		// Report unreadable entries rather than silently truncating the
		// listing, then keep going.
		w.res.Warnings = append(w.res.Warnings, fmt.Sprintf("%s: %v", path, err))
		if d != nil && d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	}

	w.unwind(w.depthFor(path))

	var parentInfo patternmatcher.MatchInfo
	if n := len(w.stack); n > 0 {
		parentInfo = w.stack[n-1].matchInfo
	}
	ignored, matchInfo, err := w.matcher.pm.MatchesUsingParentResults(path, parentInfo)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	isDir := d.IsDir()
	size := int64(0)
	if !isDir {
		info, infoErr := d.Info()
		if infoErr != nil {
			w.res.Warnings = append(w.res.Warnings, fmt.Sprintf("%s: %v", path, infoErr))
			return nil
		}
		if info.Mode().IsRegular() {
			size = info.Size()
		}
	}

	entry, err := w.newEntry(path, size, isDir, ignored)
	if err != nil {
		return err
	}

	if isDir {
		if ignored && w.canSkip {
			// Nothing inside can come back, so the directory never reaches
			// the daemon and its subtree is not worth visiting.
			return filepath.SkipDir
		}
		w.stack = append(w.stack, dirFrame{
			prefix:    path + string(filepath.Separator),
			matchInfo: matchInfo,
			entry:     entry,
			ignored:   ignored,
		})
		return nil
	}

	if ignored {
		if w.res.Summary.Ignored != nil {
			w.res.Summary.Ignored.Files++
			w.res.Summary.Ignored.Bytes += size
		}
		w.emit(entry)
		return nil
	}

	w.res.Summary.Included.Files++
	w.res.Summary.Included.Bytes += size
	w.markIncluded()
	w.emit(entry)
	return nil
}

// emit records an entry when the selected mode asks for its status.
func (w *walker) emit(entry Entry) {
	switch w.mode {
	case ModeIncluded:
		if entry.Status != StatusIncluded {
			return
		}
	case ModeIgnored:
		if entry.Status != StatusIgnored {
			return
		}
	case ModeAll:
	}
	w.res.Entries = append(w.res.Entries, entry)
}

// depthFor returns how many stack frames are still ancestors of path.
func (w *walker) depthFor(path string) int {
	n := len(w.stack)
	for n > 0 && !strings.HasPrefix(path, w.stack[n-1].prefix) {
		n--
	}
	return n
}

// markIncluded records that every open directory holds something Docker sends.
func (w *walker) markIncluded() {
	for i := range w.stack {
		w.stack[i].hasIncluded = true
	}
}

// unwind pops the stack down to depth, settling each directory's fate.
func (w *walker) unwind(depth int) {
	for len(w.stack) > depth {
		frame := w.stack[len(w.stack)-1]
		w.stack = w.stack[:len(w.stack)-1]

		switch {
		case !frame.ignored:
			// A directory Docker sends, even when empty.
			w.emit(frame.entry)
		case frame.hasIncluded:
			// Matched by a rule, but a negation rescued something inside, so
			// the directory itself has to be sent to hold it.
			frame.entry.Status = StatusIncluded
			frame.entry.Materialized = true
			w.emit(frame.entry)
		default:
			w.emit(frame.entry)
		}

		if !frame.ignored || frame.hasIncluded {
			if n := len(w.stack); n > 0 {
				w.stack[n-1].hasIncluded = true
			}
		}
	}
}

func (w *walker) newEntry(path string, size int64, isDir, ignored bool) (Entry, error) {
	entry := Entry{
		Path:   filepath.ToSlash(path),
		Status: statusOf(ignored),
		Size:   size,
		Dir:    isDir,
	}
	// Attribution costs a pass over the rules, so only pay it where a rule
	// actually decided something: every ignored path, and any included path a
	// negation could have rescued.
	if ignored || w.matcher.hasExclusions() {
		rule, err := w.matcher.decide(path)
		if err != nil {
			return Entry{}, err
		}
		if rule != nil {
			entry.Rule = rule.Raw
			entry.RuleLine = rule.Line
			entry.Negated = rule.Negated
		}
	}
	return entry, nil
}

func statusOf(ignored bool) Status {
	if ignored {
		return StatusIgnored
	}
	return StatusIncluded
}
