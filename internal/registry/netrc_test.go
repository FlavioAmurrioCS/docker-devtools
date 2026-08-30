package registry

import "testing"

func TestParseNetrc(t *testing.T) {
	// One entry per line, one spread over lines, a macdef whose body must not
	// be read as tokens, and a trailing default.
	const file = `
machine example.com login alice password s3cret

machine registry.internal
	login bob
	password hunter2

macdef init
machine attacker.example login mallory password nope

machine ghcr.io login carol password ghp_token
default login anon password anonpass
`
	cases := map[string]struct{ login, password string }{
		"example.com":       {"alice", "s3cret"},
		"registry.internal": {"bob", "hunter2"},
		"ghcr.io":           {"carol", "ghp_token"},
		// Inside a macro body, not an entry: reading it would hand another
		// host's registry the wrong credentials.
		"attacker.example": {"anon", "anonpass"},
		// No entry of its own, so the default applies.
		"unknown.example": {"anon", "anonpass"},
	}
	for host, want := range cases {
		t.Run(host, func(t *testing.T) {
			login, password := parseNetrc(file, host)
			if login != want.login || password != want.password {
				t.Errorf("parseNetrc(%q) = %q/%q, want %q/%q",
					host, login, password, want.login, want.password)
			}
		})
	}
}

func TestParseNetrcWithoutDefault(t *testing.T) {
	const file = "machine example.com login alice password s3cret\n"
	if login, password := parseNetrc(file, "other.example"); login != "" || password != "" {
		t.Errorf("parseNetrc() = %q/%q, want no credentials", login, password)
	}
}
