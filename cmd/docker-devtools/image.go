package main

import (
	"context"
	"fmt"

	"github.com/FlavioAmurrioCS/docker-devtools/internal/imgref"
	"github.com/FlavioAmurrioCS/docker-devtools/internal/imgupdate"
	"github.com/FlavioAmurrioCS/docker-devtools/internal/registry"
)

// ImageCmd groups the commands that work on image references.
type ImageCmd struct {
	// default:"withargs" so "image-refs Dockerfile" means "image-refs ls
	// Dockerfile". The cost is that a mistyped verb reads as a path:
	// "image-refs updte" reports "stat updte: no such file or directory".
	Ls     ImageLsCmd     `cmd:"" aliases:"list" default:"withargs" help:"List every image reference, with the file and line it sits on."`
	Update ImageUpdateCmd `cmd:"" help:"Rewrite image references in place."`
}

type ImageLsCmd struct {
	Paths []string `arg:"" optional:"" help:"Files or directories to scan. Defaults to the working directory."`

	JSON       bool `name:"json" help:"Emit the full result as JSON."`
	Unresolved bool `help:"Include references that cannot be resolved, such as those built from ARG values."`
}

func (c *ImageLsCmd) Run(st *Streams) error {
	res, err := imgref.Scan(c.Paths...)
	if err != nil {
		return err
	}
	for _, w := range res.Warnings {
		fmt.Fprintln(st.Stderr, "warning:", w)
	}
	if c.JSON {
		return writeJSON(st.Stdout, res)
	}
	hidden := 0
	for _, r := range res.Refs {
		if !r.Resolved && !c.Unresolved {
			hidden++
			continue
		}
		fmt.Fprintf(st.Stdout, "%s:%d\t%s", r.Path, r.Line, r.Raw)
		if !r.Resolved {
			fmt.Fprintf(st.Stdout, "\t(%s)", r.Note)
		}
		fmt.Fprintln(st.Stdout)
	}
	// Without this a file whose every reference is unresolved simply vanishes
	// from the listing, with nothing to suggest it was read at all.
	if hidden > 0 {
		fmt.Fprintf(st.Stderr, "%s not resolved; pass --unresolved to see them\n",
			plural(int64(hidden), "reference"))
	}
	return nil
}

type ImageUpdateCmd struct {
	Paths []string `arg:"" optional:"" help:"Files or directories to update. Defaults to the working directory."`

	PinDigest  bool   `help:"Append or refresh the @sha256 digest of whichever tag the reference ends on."`
	TagPolicy  string `placeholder:"POLICY" enum:"same-pattern,minor,patch,latest," default:"" help:"How far a tag may move: same-pattern, minor, patch or latest. Omit to leave tags alone."`
	DryRun     bool   `help:"Report what would change without writing anything."`
	JSON       bool   `name:"json" help:"Emit the plan as JSON."`
	FailOnDiff bool   `help:"Exit non-zero when anything would change, for use as a check."`
}

func (c *ImageUpdateCmd) Run(st *Streams) error {
	if !c.PinDigest && c.TagPolicy == "" {
		return fmt.Errorf("nothing to do: pass --pin-digest, --tag-policy, or both")
	}

	res, err := imgref.Scan(c.Paths...)
	if err != nil {
		return err
	}
	for _, w := range res.Warnings {
		fmt.Fprintln(st.Stderr, "warning:", w)
	}

	report, err := imgupdate.Plan(context.Background(), registry.New(), res.Refs, imgupdate.Options{
		PinDigest: c.PinDigest,
		Policy:    imgupdate.Policy(c.TagPolicy),
	})
	if err != nil {
		return err
	}
	for _, w := range report.Warnings {
		fmt.Fprintln(st.Stderr, "warning:", w)
	}

	if c.JSON {
		if err := writeJSON(st.Stdout, report); err != nil {
			return err
		}
	} else {
		for _, ch := range report.Changes {
			fmt.Fprintf(st.Stdout, "%s:%d\t%s -> %s\t(%s)\n", ch.Path, ch.Line, ch.Old, ch.New, ch.Reason)
		}
	}

	if !c.DryRun && len(report.Changes) > 0 {
		written, err := imgupdate.Apply(report)
		if err != nil {
			return err
		}
		fmt.Fprintf(st.Stderr, "updated %s\n", plural(int64(len(written)), "file"))
	}

	if c.FailOnDiff && len(report.Changes) > 0 {
		return fmt.Errorf("%s would change", plural(int64(len(report.Changes)), "reference"))
	}
	return nil
}
