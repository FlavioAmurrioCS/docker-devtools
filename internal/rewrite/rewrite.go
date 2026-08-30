// Package rewrite replaces byte ranges in a file without re-encoding it.
//
// Every alternative considered here re-serialises the document: a YAML
// round-trip reflows and drops comments, and regenerating a Dockerfile from its
// AST loses formatting. Splicing exact byte ranges leaves every byte outside
// the edit untouched, which is what makes an in-place update safe to run from
// a pre-commit hook.
package rewrite

import (
	"fmt"
	"sort"
)

// Edit replaces data[Start:End] with New. Old is the text expected to be
// there, and is checked before anything is written.
type Edit struct {
	Start int
	End   int
	Old   string
	New   string
}

// Apply returns data with every edit applied.
//
// It fails rather than write anything when an edit is out of bounds, overlaps
// another, or does not find Old where it expected it. A rewrite that has drifted
// from the parse is a bug, and corrupting the file would hide it.
func Apply(data []byte, edits []Edit) ([]byte, error) {
	if len(edits) == 0 {
		return data, nil
	}

	sorted := make([]Edit, len(edits))
	copy(sorted, edits)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })

	prevEnd := -1
	for _, e := range sorted {
		switch {
		case e.Start < 0 || e.End > len(data) || e.Start > e.End:
			return nil, fmt.Errorf("edit range %d..%d is outside the file (%d bytes)", e.Start, e.End, len(data))
		case e.Start < prevEnd:
			return nil, fmt.Errorf("edit at %d overlaps the previous edit ending at %d", e.Start, prevEnd)
		case string(data[e.Start:e.End]) != e.Old:
			return nil, fmt.Errorf("expected %q at %d..%d, found %q",
				e.Old, e.Start, e.End, string(data[e.Start:e.End]))
		}
		prevEnd = e.End
	}

	out := make([]byte, 0, len(data))
	cursor := 0
	for _, e := range sorted {
		out = append(out, data[cursor:e.Start]...)
		out = append(out, e.New...)
		cursor = e.End
	}
	return append(out, data[cursor:]...), nil
}
