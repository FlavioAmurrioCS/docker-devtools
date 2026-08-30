package rewrite

import (
	"strings"
	"testing"
)

// editFor builds an edit for the first occurrence of old, so the tests state
// intent rather than hand-counted byte offsets. Getting one of those wrong is
// how the first draft of this file failed.
func editFor(t *testing.T, src, old, replacement string) Edit {
	t.Helper()
	i := strings.Index(src, old)
	if i < 0 {
		t.Fatalf("%q is not in the source", old)
	}
	return Edit{Start: i, End: i + len(old), Old: old, New: replacement}
}

func TestApplySplicesExactly(t *testing.T) {
	src := "FROM alpine:3.21 AS base\nRUN true\n"
	out, err := Apply([]byte(src), []Edit{editFor(t, src, "alpine:3.21", "alpine:3.22")})
	if err != nil {
		t.Fatal(err)
	}
	if want := "FROM alpine:3.22 AS base\nRUN true\n"; string(out) != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

// The point of splicing rather than re-encoding: quoting and trailing comments
// have to survive untouched.
func TestApplyPreservesCommentsAndQuoting(t *testing.T) {
	src := "services:\n  web:\n    image: \"nginx:1.29\"   # pinned\n"
	out, err := Apply([]byte(src), []Edit{editFor(t, src, "nginx:1.29", "nginx:1.30")})
	if err != nil {
		t.Fatal(err)
	}
	want := "services:\n  web:\n    image: \"nginx:1.30\"   # pinned\n"
	if string(out) != want {
		t.Errorf("got  %q\nwant %q", out, want)
	}
}

// A range that no longer holds what the parse said it held means the two have
// drifted apart. Writing anyway would corrupt the file and hide the bug.
func TestApplyRejectsMismatchedOld(t *testing.T) {
	src := "FROM alpine:3.21\n"
	_, err := Apply([]byte(src), []Edit{{Start: 5, End: 16, Old: "debian:12", New: "x"}})
	if err == nil {
		t.Fatal("a mismatched Old should be refused")
	}
	if !strings.Contains(err.Error(), "expected") {
		t.Errorf("error should say what it expected, got: %v", err)
	}
}

func TestApplyRejectsOverlaps(t *testing.T) {
	_, err := Apply([]byte("abcdefgh"), []Edit{
		{Start: 0, End: 4, Old: "abcd", New: "x"},
		{Start: 2, End: 6, Old: "cdef", New: "y"},
	})
	if err == nil {
		t.Fatal("overlapping edits should be refused")
	}
}

func TestApplyRejectsOutOfBounds(t *testing.T) {
	if _, err := Apply([]byte("abc"), []Edit{{Start: 1, End: 99, Old: "bc", New: "z"}}); err == nil {
		t.Fatal("out-of-range edit should be refused")
	}
}

// Edits are given in arbitrary order; the result must not depend on it.
func TestApplyMultipleEditsInOneFile(t *testing.T) {
	src := "FROM a:1\nFROM b:2\n"
	out, err := Apply([]byte(src), []Edit{
		editFor(t, src, "b:2", "b:3"),
		editFor(t, src, "a:1", "a:9"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "FROM a:9\nFROM b:3\n"; string(out) != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestApplyNoEditsIsIdentity(t *testing.T) {
	src := []byte("unchanged\n")
	out, err := Apply(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(src) {
		t.Errorf("got %q", out)
	}
}
