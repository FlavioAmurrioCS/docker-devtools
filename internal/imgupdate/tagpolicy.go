package imgupdate

import (
	"regexp"
	"strconv"
	"strings"
)

// Policy decides how far a tag is allowed to move.
type Policy string

const (
	// PolicySamePattern moves only the least significant component of a tag
	// shaped like the current one, keeping the suffix and the component count.
	// "3.12-slim" becomes "3.13-slim" and "3.12.1-slim" becomes "3.12.7-slim",
	// because the first pins a minor line and the second pins a patch line.
	// Never "3.13", never "4.0-slim", and never "3.13.0-slim" from "3.12.1".
	PolicySamePattern Policy = "same-pattern"
	// PolicyMinor allows a higher minor within the current major.
	PolicyMinor Policy = "minor"
	// PolicyPatch allows a higher patch within the current major and minor.
	PolicyPatch Policy = "patch"
	// PolicyLatest allows any higher version with the same suffix, majors
	// included.
	PolicyLatest Policy = "latest"
)

// Policies lists the valid values, for flag help and validation.
var Policies = []Policy{PolicySamePattern, PolicyMinor, PolicyPatch, PolicyLatest}

func (p Policy) valid() bool {
	for _, known := range Policies {
		if p == known {
			return true
		}
	}
	return false
}

// versionTag splits a tag such as "3.12.1-alpine" into its numeric components
// and the remaining suffix.
var versionTag = regexp.MustCompile(`^v?(\d+(?:\.\d+)*)(.*)$`)

type parsedTag struct {
	parts  []int
	suffix string
	// prefix is the "v" some registries use, preserved when rebuilding.
	prefix string
}

// parseTag reports the version shape of a tag, and whether it has one at all.
// Tags like "latest", "stable" or "bookworm-slim" have no version and are never
// updated: there is no ordering to move along.
func parseTag(tag string) (parsedTag, bool) {
	m := versionTag.FindStringSubmatch(tag)
	if m == nil {
		return parsedTag{}, false
	}
	var parts []int
	for _, s := range strings.Split(m[1], ".") {
		n, err := strconv.Atoi(s)
		if err != nil {
			return parsedTag{}, false
		}
		parts = append(parts, n)
	}
	p := parsedTag{parts: parts, suffix: m[2]}
	if strings.HasPrefix(tag, "v") {
		p.prefix = "v"
	}
	return p, true
}

// newer reports whether b sorts above a. Missing components count as zero, so
// 3.12 sorts below 3.12.1.
func newer(a, b parsedTag) bool {
	n := max(len(a.parts), len(b.parts))
	for i := range n {
		av, bv := 0, 0
		if i < len(a.parts) {
			av = a.parts[i]
		}
		if i < len(b.parts) {
			bv = b.parts[i]
		}
		if av != bv {
			return bv > av
		}
	}
	return false
}

// allows reports whether moving from current to candidate is within policy.
func (p Policy) allows(current, candidate parsedTag) bool {
	if current.suffix != candidate.suffix {
		return false
	}
	if !newer(current, candidate) {
		return false
	}
	switch p {
	case PolicySamePattern:
		// Every component but the last must match, so how specific the tag is
		// decides how far it may move.
		return len(current.parts) == len(candidate.parts) &&
			sameAt(current, candidate, len(current.parts)-1)
	case PolicyMinor:
		return sameAt(current, candidate, 1)
	case PolicyPatch:
		return sameAt(current, candidate, 2)
	case PolicyLatest:
		return true
	default:
		return false
	}
}

// sameAt reports whether the first n version components match.
func sameAt(a, b parsedTag, n int) bool {
	for i := range n {
		av, bv := 0, 0
		if i < len(a.parts) {
			av = a.parts[i]
		}
		if i < len(b.parts) {
			bv = b.parts[i]
		}
		if av != bv {
			return false
		}
	}
	return true
}

// SelectTag returns the best tag to move to, or "" when none qualifies.
func SelectTag(current string, available []string, policy Policy) string {
	cur, ok := parseTag(current)
	if !ok {
		return ""
	}
	best := ""
	bestParsed := cur
	for _, candidate := range available {
		cp, ok := parseTag(candidate)
		if !ok || !policy.allows(cur, cp) {
			continue
		}
		if best == "" || newer(bestParsed, cp) {
			best, bestParsed = candidate, cp
		}
	}
	return best
}
