package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"

	"github.com/FlavioAmurrioCS/docker-devtools/internal/imgupdate"
	"github.com/FlavioAmurrioCS/docker-devtools/internal/registry"
)

// RegistryCmd groups the commands that talk to a registry. Everything else in
// this tool reads files; this is the one that needs the network.
type RegistryCmd struct {
	Tags TagsCmd `cmd:"" default:"withargs" help:"List a repository's tags, newest last."`
}

type TagsCmd struct {
	Ref string `arg:"" help:"Image reference, with or without a tag."`

	All     bool   `help:"Include every tag, not just those sharing the given tag's suffix."`
	Sort    string `enum:"version,lexical" default:"version" help:"Order tags by version, or leave the registry's own lexical order."`
	JSON    bool   `name:"json" help:"Emit the full result as JSON."`
	Current bool   `negatable:"" default:"true" help:"Mark the tag the reference names."`
}

// TagsResult is the JSON shape, which is the output to script against: the text
// form carries a marker column that would have to be stripped.
type TagsResult struct {
	Schema     int      `json:"schema"`
	Repository string   `json:"repository"`
	Current    string   `json:"current,omitempty"`
	Tags       []string `json:"tags"`
	// Unversioned holds names with no version to order, such as latest or
	// bookworm. They are reported apart rather than sorted arbitrarily.
	Unversioned []string `json:"unversioned,omitempty"`
}

func (c *TagsCmd) Run(st *Streams) error {
	parsed, err := name.ParseReference(c.Ref)
	if err != nil {
		return err
	}
	current := explicitTag(c.Ref)

	all, err := registry.New().Tags(context.Background(), c.Ref)
	if err != nil {
		return err
	}

	res := &TagsResult{
		Schema:     imgupdate.SchemaVersion,
		Repository: parsed.Context().Name(),
		Current:    current,
	}

	if c.Sort == "lexical" {
		// The spec already requires this order, so hand it back untouched.
		res.Tags = all
	} else {
		versioned, unversioned := imgupdate.SortTags(all)
		if suffix, ok := imgupdate.TagSuffix(current); ok && !c.All {
			// -alpine and -slim are different images, so a listing anchored on
			// one should not offer the other.
			versioned = withSuffix(versioned, suffix)
			unversioned = nil
		}
		res.Tags, res.Unversioned = versioned, unversioned
	}

	if c.JSON {
		return writeJSON(st.Stdout, res)
	}
	for _, t := range append(append([]string{}, res.Tags...), res.Unversioned...) {
		mark := "  "
		if c.Current && t == current {
			mark = "* "
		}
		fmt.Fprintln(st.Stdout, mark+t)
	}
	return nil
}

// withSuffix keeps only the tags whose non-numeric tail matches.
func withSuffix(tags []string, suffix string) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		if s, ok := imgupdate.TagSuffix(t); ok && s == suffix {
			out = append(out, t)
		}
	}
	return out
}

// explicitTag returns the tag the caller actually typed, or "".
//
// name.ParseReference cannot answer this: it synthesises ":latest" for a bare
// repository, so asking it would silently anchor the listing on a tag nobody
// named. A colon after the last slash is a tag; before it, a registry port.
func explicitTag(ref string) string {
	if i := strings.LastIndex(ref, "@"); i >= 0 {
		ref = ref[:i]
	}
	i := strings.LastIndex(ref, ":")
	if i < 0 || i < strings.LastIndex(ref, "/") {
		return ""
	}
	return ref[i+1:]
}
