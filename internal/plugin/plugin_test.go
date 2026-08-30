package plugin

import (
	"bytes"
	"encoding/json"
	"reflect"
	"regexp"
	"testing"
)

// TestStripPluginPrefix covers the invocation shape Docker uses: it execs a
// plugin with os.Args[1:], the full original argv minus "docker", so our own
// name arrives after Docker's global options
// (docker/cli/cli-plugins/manager/manager.go).
func TestStripPluginPrefix(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		want     []string
		isPlugin bool
	}{
		{
			name:     "bare plugin call",
			args:     []string{"devtools", "ls", "."},
			want:     []string{"ls", "."},
			isPlugin: true,
		},
		{
			name:     "boolean global flag",
			args:     []string{"--debug", "devtools", "ls"},
			want:     []string{"ls"},
			isPlugin: true,
		},
		{
			name:     "global flag with a separate value",
			args:     []string{"--context", "remote", "devtools", "ls"},
			want:     []string{"ls"},
			isPlugin: true,
		},
		{
			name:     "short global flag with a separate value",
			args:     []string{"-H", "tcp://example:2375", "devtools", "ls"},
			want:     []string{"ls"},
			isPlugin: true,
		},
		{
			name:     "global flag in equals form",
			args:     []string{"--log-level=debug", "devtools", "ls"},
			want:     []string{"ls"},
			isPlugin: true,
		},
		{
			name:     "plugin call with no arguments of our own",
			args:     []string{"devtools"},
			want:     []string{},
			isPlugin: true,
		},
		{
			name:     "direct invocation is left alone",
			args:     []string{"ls", "."},
			want:     []string{"ls", "."},
			isPlugin: false,
		},
		{
			name:     "direct invocation with our own flags",
			args:     []string{"ls", "--all", "."},
			want:     []string{"ls", "--all", "."},
			isPlugin: false,
		},
		{
			name:     "no arguments",
			args:     []string{},
			want:     []string{},
			isPlugin: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := StripPluginPrefix(tt.args)
			if ok != tt.isPlugin {
				t.Errorf("plugin invocation = %v, want %v", ok, tt.isPlugin)
			}
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("args = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestPluginMetadata pins what Docker validates when it discovers a plugin.
func TestPluginMetadata(t *testing.T) {
	var buf bytes.Buffer
	if err := WritePluginMetadata(&buf, "1.2.3"); err != nil {
		t.Fatal(err)
	}

	var meta Metadata
	if err := json.Unmarshal(buf.Bytes(), &meta); err != nil {
		t.Fatalf("metadata is not valid JSON: %v", err)
	}
	if meta.SchemaVersion != "0.1.0" {
		t.Errorf("SchemaVersion = %q, want 0.1.0 (Docker rejects anything else)", meta.SchemaVersion)
	}
	if meta.Vendor == "" {
		t.Error("Vendor is mandatory")
	}
	if meta.Version != "1.2.3" {
		t.Errorf("Version = %q, want 1.2.3", meta.Version)
	}
}

func TestPluginBinaryNameMatchesDockersRule(t *testing.T) {
	// Docker only discovers files named "docker-<name>" (metadata.NamePrefix).
	if PluginBinaryName != "docker-"+PluginName {
		t.Errorf("PluginBinaryName = %q, want docker-%s", PluginBinaryName, PluginName)
	}
	// And it rejects any name outside ^[a-z][a-z0-9]*$, which is why the
	// subcommand is "devtools" and not "docker-devtools".
	if !regexp.MustCompile(`^[a-z][a-z0-9]*$`).MatchString(PluginName) {
		t.Errorf("PluginName = %q, which Docker will refuse to load", PluginName)
	}
}
