package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/FlavioAmurrioCS/docker-devtools/dctx"
)

// ContextCmd groups the build-context commands. The listing itself is in the
// dctx package, whose output scripts/conformance.sh diffs against a real
// docker build for every fixture.
type ContextCmd struct {
	Ls      ContextLsCmd      `cmd:"" help:"List the files Docker sends as the build context."`
	Explain ContextExplainCmd `cmd:"" help:"Show which .dockerignore rule decided a path."`
}

type ContextLsCmd struct {
	Path string `arg:"" optional:"" default:"." help:"Build context directory." type:"path"`

	File    string `short:"f" placeholder:"PATH" help:"Path to the Dockerfile, relative to the context. Also selects <path>.dockerignore."`
	Ignored bool   `xor:"mode" help:"List the excluded files instead of the included ones."`
	All     bool   `xor:"mode" help:"List every file, prefixed + for sent and - for excluded."`
	Size    bool   `help:"Prefix each path with its size in bytes."`
	Summary bool   `help:"Print totals to stderr after the listing."`
	JSON    bool   `name:"json" help:"Emit the full result as JSON."`
	Zero    bool   `short:"0" help:"Separate paths with NUL, for xargs -0."`
}

func (c *ContextLsCmd) Run(st *Streams) error {
	mode := dctx.ModeIncluded
	switch {
	case c.All:
		mode = dctx.ModeAll
	case c.Ignored:
		mode = dctx.ModeIgnored
	}

	res, err := dctx.Walk(context.Background(), dctx.Options{
		Context:    c.Path,
		Dockerfile: c.File,
		Mode:       mode,
	})
	if err != nil {
		return err
	}
	for _, w := range res.Warnings {
		fmt.Fprintln(st.Stderr, "warning:", w)
	}
	if c.JSON {
		return writeJSON(st.Stdout, res)
	}

	sep := "\n"
	if c.Zero {
		sep = "\x00"
	}
	for _, e := range res.Entries {
		var b strings.Builder
		if c.All {
			if e.Status == dctx.StatusIncluded {
				b.WriteString("+ ")
			} else {
				b.WriteString("- ")
			}
		}
		if c.Size {
			fmt.Fprintf(&b, "%10d  ", e.Size)
		}
		b.WriteString(e.Path)
		b.WriteString(sep)
		if _, err := stringWrite(st.Stdout, b.String()); err != nil {
			return err
		}
	}
	if c.Summary {
		printSummary(st.Stderr, res)
	}
	return nil
}

type ContextExplainCmd struct {
	Path string `arg:"" help:"Path to explain, relative to the context or absolute. It need not exist."`

	Dir  string `short:"C" default:"." placeholder:"DIR" help:"Build context directory." type:"path"`
	File string `short:"f" placeholder:"PATH" help:"Path to the Dockerfile, relative to the context. Also selects <path>.dockerignore."`
	JSON bool   `name:"json" help:"Emit the explanation as JSON."`
}

func (c *ContextExplainCmd) Run(st *Streams) error {
	exp, err := dctx.Explain(dctx.Options{Context: c.Dir, Dockerfile: c.File}, c.Path)
	if err != nil {
		return err
	}
	for _, w := range exp.Warnings {
		fmt.Fprintln(st.Stderr, "warning:", w)
	}
	if c.JSON {
		return writeJSON(st.Stdout, exp)
	}

	fmt.Fprintf(st.Stdout, "%s: %s\n", exp.Path, exp.Status)
	if !exp.Exists {
		fmt.Fprintln(st.Stdout, "  (path does not exist in the context)")
	}
	label := exp.Ignorefile
	if label == "" {
		label = "(no ignore file)"
	}
	if len(exp.Rules) == 0 {
		fmt.Fprintf(st.Stdout, "  no rule in %s matches; included by default\n", label)
		return nil
	}
	for _, r := range exp.Rules {
		effect := "ignored"
		if r.Negated {
			effect = "re-included"
		}
		line := fmt.Sprintf("  %s:%d", label, r.Line)
		fmt.Fprintf(st.Stdout, "%-24s %-24s %s", line, r.Rule, effect)
		if r.Decisive {
			fmt.Fprint(st.Stdout, "  (decisive)")
		}
		fmt.Fprintln(st.Stdout)
	}
	return nil
}

func printSummary(w io.Writer, res *dctx.Result) {
	fmt.Fprintf(w, "included: %s, %s\n",
		plural(res.Summary.Included.Files, "file"), humanBytes(res.Summary.Included.Bytes))
	if res.Summary.Ignored != nil {
		fmt.Fprintf(w, "ignored:  %s, %s\n",
			plural(res.Summary.Ignored.Files, "file"), humanBytes(res.Summary.Ignored.Bytes))
	} else {
		fmt.Fprintln(w, "ignored:  not counted (ignored directories were skipped; use --all)")
	}
}

func plural(n int64, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func stringWrite(w io.Writer, s string) (int, error) { return io.WriteString(w, s) }
