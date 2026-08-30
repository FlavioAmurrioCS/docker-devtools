// Package imgupdate decides how image references should change and applies
// those changes in place.
package imgupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/FlavioAmurrioCS/docker-devtools/internal/imgref"
	"github.com/FlavioAmurrioCS/docker-devtools/internal/registry"
	"github.com/FlavioAmurrioCS/docker-devtools/internal/rewrite"
)

// SchemaVersion is the version of the JSON document a report marshals to.
const SchemaVersion = 1

// Options configures a plan.
type Options struct {
	// PinDigest appends or refreshes the @sha256: digest of whatever tag the
	// reference ends up on.
	PinDigest bool
	// Policy enables tag updating. Empty leaves tags alone.
	Policy Policy
}

// Change is one reference the plan would rewrite.
type Change struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Old    string `json:"old"`
	New    string `json:"new"`
	Reason string `json:"reason"`

	// start and end carry the byte range from the scan, so applying a change
	// never has to search for the text again.
	start, end int
}

// Report is the outcome of planning.
type Report struct {
	Schema   int      `json:"schema"`
	Changes  []Change `json:"changes"`
	Warnings []string `json:"warnings,omitempty"`
	// Skipped counts references left alone, so a run that changes nothing can
	// still say it looked.
	Skipped int `json:"skipped"`
}

// MarshalJSON keeps an empty plan as [] rather than null.
func (r Report) MarshalJSON() ([]byte, error) {
	type alias Report
	if r.Changes == nil {
		r.Changes = []Change{}
	}
	return json.Marshal(alias(r))
}

// Plan works out what each reference should become. It performs registry
// lookups but writes nothing.
func Plan(ctx context.Context, client registry.Client, refs []imgref.Ref, opt Options) (*Report, error) {
	if opt.Policy != "" && !opt.Policy.valid() {
		return nil, fmt.Errorf("unknown tag policy %q; expected one of %s", opt.Policy, policyList())
	}

	report := &Report{Schema: SchemaVersion}
	// Tag listings are per repository, and a file often references the same
	// repository more than once.
	tagCache := map[string][]string{}

	for _, ref := range refs {
		if !ref.Resolved {
			report.Skipped++
			continue
		}
		change, warn := planOne(ctx, client, ref, opt, tagCache)
		if warn != "" {
			report.Warnings = append(report.Warnings, warn)
			continue
		}
		if change == nil {
			report.Skipped++
			continue
		}
		report.Changes = append(report.Changes, *change)
	}

	sort.SliceStable(report.Changes, func(i, j int) bool {
		if report.Changes[i].Path != report.Changes[j].Path {
			return report.Changes[i].Path < report.Changes[j].Path
		}
		return report.Changes[i].Line < report.Changes[j].Line
	})
	return report, nil
}

func planOne(
	ctx context.Context,
	client registry.Client,
	ref imgref.Ref,
	opt Options,
	tagCache map[string][]string,
) (*Change, string) {
	base := stripTagAndDigest(ref.Raw)
	newTag := ref.Tag
	var reasons []string

	if opt.Policy != "" && ref.Tag != "" {
		tags, ok := tagCache[ref.Repository]
		if !ok {
			var err error
			tags, err = client.Tags(ctx, ref.Raw)
			if err != nil {
				return nil, fmt.Sprintf("%s:%d: %v", ref.Path, ref.Line, err)
			}
			tagCache[ref.Repository] = tags
		}
		if picked := SelectTag(ref.Tag, tags, opt.Policy); picked != "" {
			newTag = picked
			reasons = append(reasons, fmt.Sprintf("tag %s -> %s", ref.Tag, picked))
		}
	}

	next := base
	if newTag != "" {
		next += ":" + newTag
	}
	if opt.PinDigest {
		digest, err := client.Digest(ctx, next)
		if err != nil {
			return nil, fmt.Sprintf("%s:%d: %v", ref.Path, ref.Line, err)
		}
		next += "@" + digest
		if ref.Digest == "" {
			reasons = append(reasons, "pinned to "+shortDigest(digest))
		} else if ref.Digest != digest {
			reasons = append(reasons, "digest "+shortDigest(ref.Digest)+" -> "+shortDigest(digest))
		}
	}

	if next == ref.Raw {
		return nil, ""
	}
	return &Change{
		Path:   ref.Path,
		Line:   ref.Line,
		Old:    ref.Raw,
		New:    next,
		Reason: strings.Join(reasons, ", "),
		start:  ref.Start,
		end:    ref.End,
	}, ""
}

// Apply writes every change, grouped by file. It returns the files it wrote.
func Apply(report *Report) ([]string, error) {
	byFile := map[string][]rewrite.Edit{}
	for _, c := range report.Changes {
		byFile[c.Path] = append(byFile[c.Path], rewrite.Edit{
			Start: c.start, End: c.end, Old: c.Old, New: c.New,
		})
	}

	written := make([]string, 0, len(byFile))
	for path, edits := range byFile {
		info, err := os.Stat(path)
		if err != nil {
			return written, err
		}
		// The path came from a scan of what the caller asked us to update;
		// editing those files is the whole point of the command.
		data, err := os.ReadFile(path) //nolint:gosec // see above
		if err != nil {
			return written, err
		}
		out, err := rewrite.Apply(data, edits)
		if err != nil {
			return written, fmt.Errorf("%s: %w", path, err)
		}
		if err := os.WriteFile(path, out, info.Mode().Perm()); err != nil { //nolint:gosec // same path, original mode
			return written, err
		}
		written = append(written, path)
	}
	sort.Strings(written)
	return written, nil
}

// stripTagAndDigest returns the repository part of a reference as written,
// preserving the author's spelling rather than the canonical form.
func stripTagAndDigest(raw string) string {
	if i := strings.LastIndex(raw, "@"); i >= 0 {
		raw = raw[:i]
	}
	// A colon before the last slash is a registry port, not a tag.
	if i := strings.LastIndex(raw, ":"); i > strings.LastIndex(raw, "/") {
		raw = raw[:i]
	}
	return raw
}

func shortDigest(d string) string {
	if len(d) > 14 {
		return d[:14] + "…"
	}
	return d
}

func policyList() string {
	names := make([]string, len(Policies))
	for i, p := range Policies {
		names[i] = string(p)
	}
	return strings.Join(names, ", ")
}
