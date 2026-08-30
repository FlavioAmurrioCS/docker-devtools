// Package plugin implements the Docker CLI plugin contract, so the same binary
// can answer as "docker devtools".
package plugin

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Docker CLI plugin contract, from docker/cli/cli-plugins.
const (
	// PluginName is the subcommand Docker exposes. The binary must be named
	// "docker-<PluginName>" for discovery to work (metadata.NamePrefix).
	//
	// No hyphen: Docker validates plugin names against
	// pluginNameFormat = `^[a-z][a-z0-9]*$` (cli-plugins/manager/plugin.go),
	// so "build-context" is rejected outright with "plugin candidate
	// \"docker-devtools\" did not match". Hence "devtools", which also reads
	// well as "docker devtools build-context ls".
	PluginName = "devtools"

	// MetadataSubcommand is the subcommand every plugin must answer
	// (metadata.MetadataSubcommandName).
	MetadataSubcommand = "docker-cli-plugin-metadata"

	// ReexecEnvvar is set by the Docker CLI when it execs a plugin
	// (metadata.ReexecEnvvar). Its presence is a reliable signal that we were
	// invoked as a plugin rather than directly.
	ReexecEnvvar = "DOCKER_CLI_PLUGIN_ORIGINAL_CLI_COMMAND"

	// PluginBinaryName is what the file must be called inside a cli-plugins
	// directory.
	PluginBinaryName = "docker-" + PluginName
)

// Metadata is the document Docker reads to validate a plugin. Field
// names and the mandatory SchemaVersion value come from
// docker/cli/cli-plugins/metadata.Metadata.
type Metadata struct {
	SchemaVersion    string `json:"SchemaVersion"`
	Vendor           string `json:"Vendor"`
	Version          string `json:"Version,omitempty"`
	ShortDescription string `json:"ShortDescription,omitempty"`
	URL              string `json:"URL,omitempty"`
}

// WritePluginMetadata answers the docker-cli-plugin-metadata subcommand.
func WritePluginMetadata(w io.Writer, version string) error {
	enc := json.NewEncoder(w)
	return enc.Encode(Metadata{
		SchemaVersion:    "0.1.0",
		Vendor:           "Flavio Amurrio",
		Version:          version,
		ShortDescription: "Tools for Dockerfiles, Compose files and build contexts",
		URL:              "https://github.com/FlavioAmurrioCS/docker-devtools",
	})
}

// dockerGlobalFlagsWithValue are the Docker CLI global options that consume the
// following argument when not written as --flag=value. Docker passes its whole
// original argv to the plugin, so these appear before our own subcommand.
var dockerGlobalFlagsWithValue = map[string]bool{
	"--config":    true,
	"--context":   true,
	"-c":          true,
	"--host":      true,
	"-H":          true,
	"--log-level": true,
	"-l":          true,
	"--tlscacert": true,
	"--tlscert":   true,
	"--tlskey":    true,
}

// StripPluginPrefix removes the Docker global options and the plugin name from
// a plugin invocation, returning the arguments meant for us.
//
// The Docker CLI execs plugins with os.Args[1:] — the full original argv minus
// "docker" (cli-plugins/manager/manager.go) — so "docker --debug build-context
// ls ." reaches us as "--debug build-context ls .".
//
// The second return value reports whether this looked like a plugin
// invocation; when false the caller should parse args as an ordinary direct
// invocation.
func StripPluginPrefix(args []string) ([]string, bool) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == PluginName {
			return args[i+1:], true
		}
		if !strings.HasPrefix(arg, "-") {
			// A non-flag token that isn't our name: not a plugin invocation.
			return args, false
		}
		if strings.Contains(arg, "=") {
			continue
		}
		if dockerGlobalFlagsWithValue[arg] {
			i++
		}
	}
	return args, false
}

// IsPluginInvocation reports whether Docker launched us.
func IsPluginInvocation(args []string) bool {
	if os.Getenv(ReexecEnvvar) != "" {
		return true
	}
	_, ok := StripPluginPrefix(args)
	return ok
}

// Dir returns the directory a plugin should be installed into.
//
// The user directory is what Docker searches first; the system directory is
// the first of the platform defaults (cli-plugins/manager/manager_unix.go).
func Dir(system bool) (string, error) {
	if system {
		return "/usr/local/lib/docker/cli-plugins", nil
	}
	if dir := os.Getenv("DOCKER_CONFIG"); dir != "" {
		return filepath.Join(dir, "cli-plugins"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".docker", "cli-plugins"), nil
}

// InstallPlugin registers this executable as a Docker CLI plugin and returns
// the path it installed to.
//
// Python wheels cannot do this at install time: they have no post-install hook
// (PEP 517/427) and the cli-plugins directory is outside every Python scheme
// path. Hence an explicit subcommand.
//
// Unix gets a symlink so upgrading the binary upgrades the plugin. Windows has
// no dependable unprivileged symlink, so it gets a copy.
func InstallPlugin(system bool) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}

	dir, err := Dir(system)
	if err != nil {
		return "", err
	}
	// 0755, not 0750: with --system the directory lives under /usr/local and
	// every user's docker CLI has to traverse it.
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // see above
		return "", err
	}

	target := filepath.Join(dir, PluginBinaryName)
	if runtime.GOOS == "windows" {
		target += ".exe"
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return "", err
	}

	if runtime.GOOS == "windows" {
		if err := copyFile(exe, target); err != nil {
			return "", err
		}
		return target, nil
	}
	if err := os.Symlink(exe, target); err != nil {
		return "", err
	}
	return target, nil
}

// copyFile is the Windows fallback for InstallPlugin, where an unprivileged
// symlink is not dependable. Both paths are ours: the running executable and a
// cli-plugins directory we just created.
func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // src is os.Executable()
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755) //nolint:gosec // a plugin must be executable
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
