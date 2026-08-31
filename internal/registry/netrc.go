package registry

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// netrcHelper resolves credentials from a .netrc file.
//
// It satisfies authn.Helper, the one-method subset of the docker credential
// helper interface that go-containerregistry wraps with NewKeychainFromHelper.
// That is the whole integration: the docker keychain already covers
// config.json, credsStore, credHelpers, $REGISTRY_AUTH_FILE and podman's
// auth.json, so netrc only has to fill the gap for people who keep registry
// credentials the way curl and git do.
type netrcHelper struct{}

// Get returns the login and password for a registry host, or empty strings when
// the file has no entry for it. A missing or unreadable file is not an error:
// it just means no credentials, and the keychain moves on to anonymous access.
func (netrcHelper) Get(serverURL string) (string, string, error) {
	path := netrcPath()
	if path == "" {
		return "", "", nil
	}
	// A missing or unreadable netrc is not an error: it means no credentials,
	// and the keychain moves on to the next source or to anonymous access.
	data, err := os.ReadFile(path) //nolint:gosec // see above
	if err != nil {
		return "", "", nil //nolint:nilerr // see above
	}

	host := serverURL
	if i := strings.Index(host, "/"); i >= 0 {
		host = host[:i]
	}
	if i := strings.Index(host, ":"); i >= 0 {
		host = host[:i]
	}
	login, password := parseNetrc(string(data), host)
	return login, password, nil
}

// netrcPath returns the file to read, honouring $NETRC the way curl does.
func netrcPath() string {
	if p := os.Getenv("NETRC"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	name := ".netrc"
	if runtime.GOOS == "windows" {
		name = "_netrc"
	}
	return filepath.Join(home, name)
}

// parseNetrc finds the credentials for host, falling back to a "default" entry.
//
// The format is whitespace-separated tokens rather than records, so an entry
// may be written on one line or spread over several. The one place lines matter
// is "macdef", whose macro body runs to the next blank line and holds no
// credentials, so it is skipped whole. That is also why this reads the file
// rather than streaming it: a token scanner cannot see the blank line that ends
// a macro.
func parseNetrc(data, host string) (login, password string) {
	lines := strings.Split(data, "\n")
	var inMatch, inDefault, matched bool

	for i := 0; i < len(lines); i++ {
		fields := strings.Fields(lines[i])
		for j := 0; j < len(fields); j++ {
			next := func() string {
				if j+1 < len(fields) {
					j++
					return fields[j]
				}
				return ""
			}
			switch fields[j] {
			case "machine":
				inMatch = next() == host
				inDefault = false
				matched = matched || inMatch
			case "default":
				// Only reachable when no machine entry matched; it comes last.
				inDefault = !matched
				inMatch = false
			case "macdef":
				i = endOfMacro(lines, i)
				inMatch, inDefault = false, false
				j = len(fields)
			case "login":
				if inMatch || inDefault {
					login = next()
				}
			case "password":
				if inMatch || inDefault {
					password = next()
				}
			}
			if inMatch && login != "" && password != "" {
				return login, password
			}
		}
	}
	return login, password
}

// endOfMacro returns the index of the blank line closing a macro body, so the
// caller resumes after it. A macro holds shell commands, not credentials, and
// reading them as tokens would hand one host's password to another.
func endOfMacro(lines []string, start int) int {
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			return i
		}
	}
	return len(lines)
}
