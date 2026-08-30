// Command docker-devtools works on the Docker files in a repository: the build
// context a Dockerfile would send, and the image references it and any Compose
// files point at.
//
// The same binary doubles as a Docker CLI plugin. Install it into a cli-plugins
// directory and it answers as "docker devtools".
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/alecthomas/kong"
	kongusage "github.com/gaojunran/usage-integrations/packages/kong-usage"

	"github.com/FlavioAmurrioCS/docker-devtools/internal/plugin"
)

// version is overwritten at build time with -X main.version=...
var version = "dev"

const binaryName = "docker-devtools"

// Streams carries the writers commands emit to, so tests can capture them.
// It is named Streams rather than IO so that it never shadows the io package
// inside a command's Run method.
type Streams struct {
	Stdout io.Writer
	Stderr io.Writer
}

// CLI is the whole command tree. kong derives parsing, help and, through
// kong-usage, the KDL spec that drives shell completions.
type CLI struct {
	Context ContextCmd `cmd:"" help:"Inspect the build context a Dockerfile would send."`
	Image   ImageCmd   `cmd:"" help:"Find and update image references."`

	InstallDockerPlugin InstallPluginCmd `cmd:"" name:"install-docker-plugin" help:"Register this binary as the \"docker devtools\" plugin."`
	Version             VersionCmd       `cmd:"" help:"Print the version."`
}

type VersionCmd struct{}

func (c *VersionCmd) Run(st *Streams) error {
	fmt.Fprintln(st.Stdout, version)
	return nil
}

type InstallPluginCmd struct {
	System bool `help:"Install for all users instead of the current one."`
}

func (c *InstallPluginCmd) Run(st *Streams) error {
	path, err := plugin.InstallPlugin(c.System)
	if err != nil {
		return err
	}
	fmt.Fprintf(st.Stdout, "installed %s\nrun it with: docker %s context ls\n", path, plugin.PluginName)
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		var parseErr *kong.ParseError
		if errors.As(err, &parseErr) {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, binaryName+":", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	// Docker hands plugins its whole original argv, global flags and all.
	if stripped, ok := plugin.StripPluginPrefix(args); ok {
		args = stripped
	}

	// Answered before kong sees the arguments, because neither is part of the
	// command tree: one is Docker's plugin handshake, the other feeds the
	// completion generator.
	if len(args) > 0 {
		switch args[0] {
		case plugin.MetadataSubcommand:
			return plugin.WritePluginMetadata(stdout, version)
		case "--usage-spec":
			parser, err := newParser(stdout, stderr)
			if err != nil {
				return err
			}
			fmt.Fprint(stdout, kongusage.GenerateKDL(parser, binaryName))
			return nil
		}
	}

	parser, err := newParser(stdout, stderr)
	if err != nil {
		return err
	}
	kctx, err := parser.Parse(args)
	if err != nil {
		parser.Errorf("%s", err)
		return err
	}
	return kctx.Run(&Streams{Stdout: stdout, Stderr: stderr})
}

func newParser(stdout, stderr io.Writer) (*kong.Kong, error) {
	var cli CLI
	return kong.New(&cli,
		kong.Name(binaryName),
		kong.Description("Work on the Dockerfiles, Compose files and build context in a repository."),
		kong.UsageOnError(),
		kong.Writers(stdout, stderr),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
	)
}
