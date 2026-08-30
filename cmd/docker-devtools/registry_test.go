package main

import "testing"

// A colon means a tag only after the last slash; before it, it is a registry
// port. name.ParseReference cannot answer this, because it synthesises
// ":latest" for a bare repository.
func TestExplicitTag(t *testing.T) {
	cases := map[string]string{
		"python:3.12-slim":                 "3.12-slim",
		"python":                           "",
		"ghcr.io/acme/api:v1":              "v1",
		"ghcr.io/acme/api":                 "",
		"localhost:5000/api":               "",
		"localhost:5000/api:v2":            "v2",
		"alpine:3.20@sha256:" + zeroDigest: "3.20",
		"alpine@sha256:" + zeroDigest:      "",
	}
	for ref, want := range cases {
		if got := explicitTag(ref); got != want {
			t.Errorf("explicitTag(%q) = %q, want %q", ref, got, want)
		}
	}
}

const zeroDigest = "0000000000000000000000000000000000000000000000000000000000000000"
