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
	Ls ContextLsCmd `cmd:"" aliases:"list" default:"withargs" help:"List the files Docker sends as the build context."`
}

type ContextLsCmd struct {
	// No type:"path": kong would absolutise this before dctx sees it, and every
	// diagnostic below would then quote a path the caller never typed. dctx
	// absolutises internally where it matters.
	Path string `arg:"" optional:"" default:"." help:"Build context directory."`

	File   string `short:"f" placeholder:"PATH" help:"Path to the Dockerfile, relative to the current directory (default: PATH/Dockerfile). Also selects <path>.dockerignore."`
	Target string `placeholder:"STAGE" help:"Stage to build, as docker build --target selects it. Changes which COPY sources are reachable."`
	// A build sends only the paths its Dockerfile names, so the two sets differ
	// for every Dockerfile that does not copy the context whole.
	WholeContext bool `help:"List everything the ignore rules permit, not just what the Dockerfile pulls in."`

	Ignored bool `xor:"mode" help:"List the excluded files instead of the included ones."`
	All     bool `xor:"mode" help:"List every file, prefixed + for sent and - for excluded."`
	Size    bool `help:"Prefix each path with its size in bytes."`
	Why     bool `xor:"format" help:"Append the ignore-file rule that decided each path."`
	Summary bool `help:"Print totals to stderr after the listing."`
	JSON    bool `name:"json" help:"Emit the full result as JSON."`
	// --why annotates each line, which would corrupt output meant for xargs.
	Zero bool `short:"0" xor:"format" help:"Separate paths with NUL, for xargs -0."`
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
		Context:      c.Path,
		Dockerfile:   c.File,
		Target:       c.Target,
		WholeContext: c.WholeContext,
		Mode:         mode,
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
		if c.Why && e.Rule != "" {
			fmt.Fprintf(&b, "  <- %s:%d %s", res.Ignorefile, e.RuleLine, e.Rule)
		}
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

func printSummary(w io.Writer, res *dctx.Result) {
	// The gap between what is transferred and what the ignore rules permit is
	// the interesting number: it is the difference between a 458 MiB context
	// and the one file a Dockerfile actually reads.
	if t := res.Summary.Transferred; t != nil {
		fmt.Fprintf(w, "transferred: %s, %s\n", plural(t.Files, "file"), humanBytes(t.Bytes))
		fmt.Fprintf(w, "permitted:   %s, %s\n",
			plural(res.Summary.Included.Files, "file"), humanBytes(res.Summary.Included.Bytes))
	} else {
		fmt.Fprintf(w, "transferred: %s, %s (the Dockerfile copies the context whole)\n",
			plural(res.Summary.Included.Files, "file"), humanBytes(res.Summary.Included.Bytes))
	}
	if res.Summary.Ignored != nil {
		fmt.Fprintf(w, "ignored:     %s, %s\n",
			plural(res.Summary.Ignored.Files, "file"), humanBytes(res.Summary.Ignored.Bytes))
	} else {
		fmt.Fprintln(w, "ignored:     not counted (ignored directories were skipped; use --all)")
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
