package imgupdate

import (
	"reflect"
	"strings"
	"testing"
)

// The registry tag list a python image actually offers, trimmed. Real lists
// mix shapes, which is the whole reason same-pattern is the default.
var pythonTags = []string{
	"3.11", "3.11-slim", "3.11.9", "3.11.9-slim",
	"3.12", "3.12-slim", "3.12.1", "3.12.1-slim", "3.12.7", "3.12.7-slim",
	"3.13", "3.13-slim", "3.13.0", "3.13.0-slim",
	"4.0", "4.0-slim",
	"latest", "slim", "bookworm", "slim-bookworm", "alpine",
}

func TestSelectTag(t *testing.T) {
	cases := []struct {
		name    string
		current string
		policy  Policy
		want    string
	}{
		{"same pattern keeps the suffix and the shape", "3.12-slim", PolicySamePattern, "3.13-slim"},
		{"same pattern will not drop the suffix", "3.12", PolicySamePattern, "3.13"},
		{"a patch-pinned tag moves only its patch", "3.12.1-slim", PolicySamePattern, "3.12.7-slim"},
		{"same pattern will not cross a major", "3.13-slim", PolicySamePattern, ""},
		{"minor stays inside the major", "3.11-slim", PolicyMinor, "3.13-slim"},
		{"patch stays inside the minor", "3.12.1-slim", PolicyPatch, "3.12.7-slim"},
		// patch compares components, not shapes: a missing component counts as
		// zero, so a tag pinning no patch at all may grow one. Only
		// same-pattern keeps the shape. The README's policy table says so.
		{"patch may lengthen a tag that pins no patch", "3.12-slim", PolicyPatch, "3.12.7-slim"},
		{"latest may cross a major", "3.12-slim", PolicyLatest, "4.0-slim"},
		{"an unversioned tag never moves", "latest", PolicySamePattern, ""},
		{"a codename tag never moves", "bookworm", PolicyLatest, ""},
		{"already newest yields nothing", "4.0-slim", PolicyLatest, ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := SelectTag(tt.current, pythonTags, tt.policy); got != tt.want {
				t.Errorf("SelectTag(%q, %s) = %q, want %q", tt.current, tt.policy, got, tt.want)
			}
		})
	}
}

// A suffix is part of the identity of a tag: -alpine and -slim are different
// images, and moving between them would change the base distro silently. No
// policy may do it, whatever else it picks.
func TestSelectTagNeverChangesSuffix(t *testing.T) {
	tags := []string{"1.29-alpine", "1.30-alpine", "1.31", "1.31-slim", "2.0-alpine"}
	for _, p := range Policies {
		got := SelectTag("1.29-alpine", tags, p)
		if got == "" {
			continue // refusing to move is always allowed
		}
		if !strings.HasSuffix(got, "-alpine") {
			t.Errorf("policy %s picked %q, which changes the suffix", p, got)
		}
	}
}

func TestParseTag(t *testing.T) {
	cases := map[string]struct {
		ok     bool
		parts  int
		suffix string
	}{
		"3.12":          {true, 2, ""},
		"3.12-slim":     {true, 2, "-slim"},
		"3.12.1":        {true, 3, ""},
		"v1.2.3":        {true, 3, ""},
		"1":             {true, 1, ""},
		"latest":        {false, 0, ""},
		"bookworm":      {false, 0, ""},
		"alpine3.22":    {false, 0, ""},
		"7.4-alpine":    {true, 2, "-alpine"},
		"bookworm-2024": {false, 0, ""},
	}
	for tag, want := range cases {
		got, ok := parseTag(tag)
		if ok != want.ok {
			t.Errorf("parseTag(%q) ok = %v, want %v", tag, ok, want.ok)
			continue
		}
		if !ok {
			continue
		}
		if len(got.parts) != want.parts || got.suffix != want.suffix {
			t.Errorf("parseTag(%q) = parts %v suffix %q, want %d parts suffix %q",
				tag, got.parts, got.suffix, want.parts, want.suffix)
		}
	}
}

func TestNewerTreatsMissingComponentsAsZero(t *testing.T) {
	a, _ := parseTag("3.12")
	b, _ := parseTag("3.12.1")
	if !newer(a, b) {
		t.Error("3.12.1 should sort above 3.12")
	}
	if newer(b, a) {
		t.Error("3.12 should not sort above 3.12.1")
	}
}

func TestSortTags(t *testing.T) {
	// Deliberately in the lexical order a registry is required to return, which
	// is the order that puts 3.10 before 3.9 and 2.6 before 20190228.
	in := []string{
		"2.6", "20190228", "3.1", "3.10", "3.10-slim", "3.9", "3.9-slim",
		"edge", "latest",
	}
	versioned, unversioned := SortTags(in)

	want := []string{"2.6", "3.1", "3.9", "3.9-slim", "3.10", "3.10-slim", "20190228"}
	if !reflect.DeepEqual(versioned, want) {
		t.Errorf("SortTags() versioned =\n%q\nwant\n%q", versioned, want)
	}
	if wantRest := []string{"edge", "latest"}; !reflect.DeepEqual(unversioned, wantRest) {
		t.Errorf("SortTags() unversioned = %q, want %q", unversioned, wantRest)
	}
}

func TestTagSuffix(t *testing.T) {
	cases := map[string]struct {
		suffix string
		ok     bool
	}{
		"3.12-slim":   {"-slim", true},
		"3.12.1-slim": {"-slim", true},
		"3.12":        {"", true},
		"v1.2.3":      {"", true},
		"latest":      {"", false},
		"bookworm":    {"", false},
	}
	for tag, want := range cases {
		got, ok := TagSuffix(tag)
		if got != want.suffix || ok != want.ok {
			t.Errorf("TagSuffix(%q) = %q, %v; want %q, %v", tag, got, ok, want.suffix, want.ok)
		}
	}
}
