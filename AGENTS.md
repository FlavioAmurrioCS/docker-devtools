# AGENTS.md

Conventions for coding agents working in this repo.

This is a Go CLI that works on the Docker files in a repository: the build
context a Dockerfile would send, and the image references in Dockerfiles and
Compose files. It is distributed as Python wheels and as a Docker CLI plugin.

## Hard rules

1. **Never reimplement what Docker already defines.** Dockerfile parsing comes
   from `moby/buildkit`, image references and registry access from
   `google/go-containerregistry`, and `.dockerignore` matching and context
   walking from `moby/patternmatcher` and `tonistiigi/fsutil`. If you find
   yourself writing a pattern matcher or a reference parser, stop.

2. **`scripts/conformance.sh` is the source of truth for `dctx`.** It builds
   each fixture with `FROM scratch` and `COPY . /`, exports the tarball, and
   diffs the members against `build-context ls`. When it disagrees with a unit test,
   the unit test is wrong. It is what caught the materialised-directory
   behaviour that every other check missed. Run it after touching matching or
   walking.

3. **Never re-encode a file to edit it.** `internal/rewrite` splices exact byte
   ranges. A YAML round-trip reflows the document and drops comments, and
   regenerating a Dockerfile from its AST loses formatting. It is why each
   scanner records a byte offset for every reference, and why `compose-go` is
   not used: it resolves variables and validates, so what it returns no longer
   matches the bytes on disk.

4. **A rewrite that cannot find what it expects must fail, not write.**
   `rewrite.Apply` checks every range against the text the parse recorded. Drift
   between the two is a bug, and writing anyway would hide it inside a corrupted
   file.

5. **Never run `git push`.** That decision belongs to a human, every time.

6. **Suggest the commit and wait.** One logical change per commit, named so
   `git log --oneline` reads as a history.

## The delicate parts

- `internal/imgref` decides what counts as an image. A stage name, a stage
  index and `scratch` are reported as unresolved rather than guessed at.
  `TestByteRangesLandOnTheReference` is the safety net: every resolved
  reference's byte range must contain exactly its own text.
- A `FROM` behind a meta `ARG` is expanded with buildkit's own `shell.Lex`, the
  way `dockerfile2llb` does it, and anchored on the ARG line. The rule that
  keeps this safe is the byte-range invariant above, not a special case: the
  expansion must appear verbatim on an ARG line, so `ARG BASE=debian:13-slim`
  resolves and `ARG VERSION=12` with `FROM debian:${VERSION}-slim` does not.
  One ARG may back several `FROM`s, so the reference is emitted once; two edits
  over one range would reach `rewrite.Apply`, which is right to reject them.
- `internal/imgupdate` holds the tag policy. `same-pattern` moves only the last
  version component and never changes the suffix, because `-alpine` and `-slim`
  are different images. `TestSelectTagNeverChangesSuffix` pins that for every
  policy.
- `internal/rewrite` is small on purpose. Read it before changing anything that
  writes to disk.
- `dctx` is the one package here with a public import path, because the walker
  is worth reusing on its own. Treat its exported names as API.
- A Dockerfile is required. `Dockerfile` then lowercase `dockerfile` is the whole
  candidate set, matching buildkit's `frontend/dockerui`; there is deliberately
  no `Containerfile` fallback, even though `imgref.FileKind` accepts that name.
  What docker opens by default and what is worth scanning are different
  questions. A context with no ignore file warns rather than failing: the
  listing is still true, it is just the whole tree.
- `-f` is a path resolved from the working directory, not from the context, and
  the ignore file is the one beside the Dockerfile. That is docker's rule, stated
  outright in docker/cli `cli/command/image/build/context.go`, and produced by
  buildx (`build/opt.go` mounts `filepath.Dir(-f)` as a separate "dockerfile"
  input) plus the frontend (buildkit `frontend/dockerui/config.go` reads
  `filename + ".dockerignore"` from it, and falls back to the context root only
  when that came up empty). `testdata/dctx/outofcontext` pins it against a real
  build; its decoy ignore file inside the context fails the fixture loudly if
  resolution ever drifts back to context-relative.
- We conform to BuildKit, not the legacy builder. `docker build` forwards to
  buildx by default (docker/cli `cmd/docker/builder.go`), and the legacy builder
  never supported `<dockerfile>.dockerignore` at all.

`internal/registry` authenticates through `authn.DefaultKeychain`, which already
covers `config.json` and its `credsStore`/`credHelpers` shell-outs,
`$REGISTRY_AUTH_FILE` and podman's `auth.json`. `netrc.go` sits behind it in a
`MultiKeychain`, so a `docker login` always beats a stale netrc entry.

Tag ordering lives in `internal/imgupdate`, not in the command: the OCI
distribution spec requires the registry to return tags lexically and carries no
timestamps, so `SortTags` is the only ordering there is. Do not add a
publication-date sort. It costs three requests per tag, and reproducible builds
set the date to the epoch.

Registry tests use `go-containerregistry`'s in-process registry
(`pkg/registry`) with synthetic images (`pkg/v1/random`). Add tests there rather
than against real registries: the suite has to stay hermetic.

## kong has no completion, and that is deliberate

kong doesn't provide shell completion, and [its issue asking for
one](https://github.com/alecthomas/kong/issues/43) has been open since 2019.
`kongplete` is not the answer: it pins kong v0.8.1 against the current v1.x.

Instead the binary answers `--usage-spec` with a KDL document generated from
kong's own model by `kong-usage`, and the [usage](https://usage.jdx.dev) CLI
turns that into completions for five shells, plus a markdown reference.
`mise run completions` does it.

The generated scripts call back to `usage` at completion time, so users need it
on PATH. `--usage-spec` is answered before
kong parses anything, next to Docker's `docker-cli-plugin-metadata` handshake,
because neither belongs in the command tree.

## Schema versions

`imgref.SchemaVersion` and `imgupdate.SchemaVersion` are in the JSON output, and
`IMAGE_SCHEMA_VERSION` in `src/docker_devtools/__init__.py` is checked against
them. Bump them together: the Python wrapper refuses a document it does not
recognise.

## Tooling

mise.toml defines the tools and the tasks.

```sh
mise run build         # compile into ./build
mise run test          # go test + pytest
mise run lint          # pre-commit across the repo
mise run prose         # vale-ai-tells across all markdown
mise run completions   # regenerate completions and docs
mise run wheels        # every platform wheel into ./dist
mise run test-clone    # verify a fresh clone in a container
```

Shell scripts start with `eval "$(mise env -s bash)"`, because `mise activate`
is a prompt hook that never fires in a non-interactive shell. They must pass
`shellcheck` and `shfmt -i 2 -ci`. No `mapfile`: macOS still has bash 3.2.

Run `mise run test-clone` after touching `mise.toml`, `pyproject.toml`,
`hatch_build.py`, `.pre-commit-config.yaml` or anything in `scripts/`. A temp
directory on the development machine inherits a working Go toolchain, a warm
module cache and an existing uv, and hides the failures that matter.

## Packaging

The wheel holds the Go binary in `.data/scripts` and the Python package beside
it, which is the layout `uv` uses. There is deliberately no `[project.scripts]`
entry: a `console_scripts` shim would add interpreter startup to every
invocation, and this runs under pre-commit.

`hatch_build.py` builds one target per invocation, chosen by `DDT_TARGET` and
defaulting to the host so a plain `uv build` works. The version comes from git
tags through hatch-vcs and reaches the binary through `-X main.version`.

The distribution name, the binary name and the repo name are all
`docker-devtools`, which is what makes `uvx docker-devtools` and `pipx run
docker-devtools` work without configuration. Do not add a short alias.

The Docker plugin subcommand is `devtools`, with no hyphen, because Docker
validates plugin names against `^[a-z][a-z0-9]*$`. That rule applies to the
plugin name only, so `build-context` and `image-refs` may hyphenate.
Both are named away from `context` and `image` because `docker context ls` and
`docker image ls` are real commands meaning something else entirely.

## Prose is linted

`vale-ai-tells` runs on changed markdown in pre-commit and on commit messages
via the `commit-msg` hook. `mise run prose` lints everything. The style packs
are gitignored and `scripts/prose-lint.sh` fetches them on first run.

## Writing style

Write for someone debugging a slow build at 2am.

- Say what a thing is for in the first line.
- Record the gotcha. The comment explaining why a byte range is spliced rather
  than a document re-encoded is worth more than the code around it.
- Cite upstream when a behaviour is inherited: file and line, so the next reader
  can check it still holds.
- An empty section beats filler.
