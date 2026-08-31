# docker-devtools

Work on the Docker files in a repository: the build context a Dockerfile would
send, and the image references it and your Compose files point at.

```console
$ docker-devtools image-refs ls
Dockerfile:1        python:3.11-slim
compose.yaml:3      nginx:1.25-alpine

$ docker-devtools image-refs update --tag-policy same-pattern --dry-run
Dockerfile:1     python:3.11-slim -> python:3.14-slim      (tag 3.11-slim -> 3.14-slim)
compose.yaml:3   nginx:1.25-alpine -> nginx:1.31-alpine    (tag 1.25-alpine -> 1.31-alpine)
```

## Why another one

Renovate and Dependabot already update image references, and they do it well.
They run as bots against a repository and open pull requests. This one runs on
your machine and edits the files in place. It is fast enough for a pre-commit
hook, so a stale base image gets caught before it is ever committed.

Where the semantics are Docker's, this defers to Docker's own code:

| Step | Package |
| --- | --- |
| Parse Dockerfiles | `moby/buildkit/frontend/dockerfile/parser` and `instructions` |
| Parse image references | `google/go-containerregistry/pkg/name` |
| Talk to registries | `google/go-containerregistry/pkg/v1/remote` |
| Match .dockerignore rules | `moby/patternmatcher` |
| Walk a build context | `tonistiigi/fsutil`, the package BuildKit sends contexts with |

None of the `.dockerignore` semantics are reimplemented here, and CI checks
that rather than asserting it: for every fixture, `scripts/conformance.sh`
builds `FROM scratch` with `COPY . /`, exports the image as a tarball, and
diffs the tar members against what `build-context ls` reports.

## Install

```console
$ uvx docker-devtools image-refs ls          # no install
$ pipx run docker-devtools image-refs ls     # no install
$ uv tool install docker-devtools
$ pip install docker-devtools
$ mise use ubi:FlavioAmurrioCS/docker-devtools
```

Prebuilt binaries are attached to each
[release](https://github.com/FlavioAmurrioCS/docker-devtools/releases). With a
Go toolchain:

```console
$ go install github.com/FlavioAmurrioCS/docker-devtools/cmd/docker-devtools@latest
```

Reading and updating files doesn't require a Docker installation or a running
daemon.
Registry lookups authenticate with the same `~/.docker/config.json` the docker
CLI uses.

The build context is what BuildKit would send, because `docker build` forwards to
buildx by default. The legacy builder never learned `<dockerfile>.dockerignore`
at all. Its `ReadDockerignore` opens only `<context>/.dockerignore`, so a
`DOCKER_BUILDKIT=0` build legitimately disagrees with this listing whenever a
per-Dockerfile ignore file is in play.

## As a pre-commit hook

```yaml
repos:
  - repo: https://github.com/FlavioAmurrioCS/docker-devtools
    rev: v0.0.1
    hooks:
      - id: docker-image-check
```

The hooks are scoped to `Dockerfile*`, `Containerfile*` and
`(docker-)?compose*.ya?ml` already:

| Hook | What it does |
| --- | --- |
| `docker-image-check` | Fails when a tag could move. Writes nothing. |
| `docker-image-update` | Moves tags in place, under `same-pattern`. |
| `docker-image-pin` | Appends or refreshes the `@sha256` digest. |

Each one reaches the registry, so a hook is only as fast as the tag listing it
asks for. `docker-image-check` is the one to reach for first: it reports without
touching the tree.

pre-commit installs a `repo:` from source, and building this one compiles the Go
binary, so the machine running the hook needs a Go toolchain. Somewhere that
cannot have one, install the published wheel instead and point a `local` hook at
the `docker-devtools` it puts on PATH.

## Usage

```text
docker-devtools build-context ls [PATH]      list the files Docker would send
docker-devtools image-refs ls [PATH...]      list every image reference, with file and line
docker-devtools image-refs update [PATH...]  rewrite references in place
docker-devtools registry tags REF            list a repository's tags, newest last
docker-devtools install-docker-plugin        register as "docker devtools"
docker-devtools version                      print the version (also --version)
```

Each group's `ls` is also the default, so `image-refs Dockerfile` and
`build-context .` work without it. The cost is that a mistyped verb reads as a
path: `image-refs updte` reports `stat updte: no such file or directory`.

`ls` is also spelled `list`. The groups are named away from `context` and
`image` on purpose: `docker context ls` lists CLI endpoints and `docker image ls`
lists local images, and neither is anything like what these do.

`PATH` is the build context directory for `build-context ls`, and files or
directories to scan for `image-refs`. Every command takes `--json`, which is the
same document the Python API parses.

`build-context ls` lists directories in their own right, the way `docker build`
sends them, so a listing piped through `-0` into `xargs` gets both. It also
takes:

```text
-f, --file PATH   Dockerfile to derive <path>.dockerignore from
    --ignored     list what the ignore file excluded instead
    --all         list everything, prefixed + for sent and - for excluded
    --size        prefix each path with its size in bytes
    --why         append the ignore-file rule that decided each path
    --target STAGE  build as if --target were given, changing what is reached
    --whole-context list everything the ignore rules permit
    --summary     print totals to stderr after the listing
-0, --zero        separate paths with NUL
```

### What actually gets sent

BuildKit transfers only the paths the Dockerfile names. Every `COPY` and `ADD`
source becomes a follow path on the build context, so a Dockerfile that copies
one file transfers one file, however large the directory around it:

```console
$ docker-devtools build-context ls --summary
taplo.toml
transferred: 1 file, 1.7 KiB
permitted:   24626 files, 458.5 MiB
```

The gap between those two lines is the point. `permitted` is what the ignore
rules allow through, which is also what gets sent when something copies the
context whole: `COPY . /` switches the filter off. `--whole-context` lists that
set, and `--target` picks the stage, since a stage the build never reaches never
reads its sources.

### Which Dockerfile, and which .dockerignore

A context needs a Dockerfile. With no `-f`, `Dockerfile` is looked for and then
the lowercase `dockerfile`, which is the whole candidate set BuildKit uses;
there is no `Containerfile` fallback. When neither is there the command fails,
because `docker build` would too, and a listing of a build that cannot run
describes nothing.

A context with no `.dockerignore` at all says so, since that is the reason
`.git` and a virtualenv turn up in the listing:

```console
$ docker-devtools build-context ls
warning: no .dockerignore in .; every file is sent
```

`-f` takes a path, resolved from your working directory rather than from the
context. That is the rule `docker build -f` follows, and the Dockerfile may sit
outside the context entirely. The ignore file is the one **beside the
Dockerfile**, `<path>.dockerignore`, falling back to `<context>/.dockerignore`.
The first wins outright. They never merge.

```console
$ docker-devtools build-context ls -f docker/build.Dockerfile ./app
  # reads docker/build.Dockerfile.dockerignore, else app/.dockerignore
```

`--why` says which rule decided each path, which is usually the question:

```console
$ docker-devtools build-context ls --all --why
+ app.js
- node_modules/drop/index.js  <- .dockerignore:1 node_modules
+ node_modules/keep/index.js  <- .dockerignore:2 !node_modules/keep
```

### Updating image references

What changes is split by how much judgement it needs.

`--pin-digest` resolves the current tag to a digest and appends it, turning
`nginx:1.29` into `nginx:1.29@sha256:…`. It doesn't decide anything about versions, so it is
reversible and safe to run anywhere.

`--tag-policy` moves the tag. The default, `same-pattern`, moves only the last
component and keeps the suffix, so how specific your tag is decides how far it
may move:

| Current tag | same-pattern | minor | patch | latest |
| --- | --- | --- | --- | --- |
| `3.12-slim` | `3.13-slim` | `3.13-slim` | `3.12.7-slim` | `4.0-slim` |
| `3.12.1-slim` | `3.12.7-slim` | `3.13.0-slim` | `3.12.7-slim` | `4.0-slim` |
| `latest` | no change | no change | no change | no change |

Only `same-pattern` keeps the shape of a tag. The other three compare version
components, and a component the current tag omits counts as zero, so `patch` can
turn `3.12-slim` into `3.12.7-slim`: a tag that pinned a minor line now pins a
patch.

No policy ever changes the suffix: `-alpine` and `-slim` are different images,
and swapping them would change your base distribution without saying so. Tags
with no version, such as `latest` or `bookworm`, are never moved, because there
is no ordering to move along.

Add `--dry-run` to see the plan without writing, and `--fail-on-diff` to exit
non-zero when anything would change, which is what makes it useful in CI.

`--fail-on-diff` reports on the plan, not on the writing, so on its own it
still rewrites the files and then exits non-zero. Pair it with `--dry-run` for a
check that leaves the tree alone, which is what the `docker-image-check` hook
does.

### Base images behind an ARG

A Dockerfile that opens `ARG BASE_IMAGE=debian:13-slim` and then
`FROM "${BASE_IMAGE}"` still has a real base image, and it is updatable. The
`FROM` is expanded through the ARG defaults with BuildKit's own lexer, the same
way `docker build` does it, and the reference is reported on the **ARG** line,
because that is the only text an update can rewrite:

```console
$ docker-devtools image-refs ls --unresolved
Dockerfile:1   debian:13-slim
Dockerfile:5   "${BASE_IMAGE}"   (resolved from ARG BASE_IMAGE on line 1)
```

This holds only when the ARG default is the whole reference, spelled out on its
own line. `ARG VERSION=12` with `FROM debian:${VERSION}-slim` stays unresolved:
the image is `debian:12-slim`, which is written nowhere, and rewriting would
mean splicing a bare tag into the middle of a line.

### What it will not touch

Some references cannot be resolved to an image, and those are reported rather
than guessed at. Pass `--unresolved` to `image-refs ls` to see them:

- `FROM builder`, where `builder` is an earlier stage
- `COPY --from=0`, which indexes a stage
- `FROM $BASE` where the ARG has no default, or supplies only part of the
  reference
- `FROM scratch`, which is the empty base rather than a registry image
- Compose values built from variables, such as `${REGISTRY}/app:latest`

A listing says how many it withheld, so a file whose every reference is one of
these does not simply vanish from the output.

### Editing in place

An update splices the new reference into the exact byte range the parser
reported. It never re-encodes the file, so comments, quoting style, anchors and
whitespace all survive:

```yaml
    image: "nginx:1.29-alpine"   # keep this comment and the quotes
```

becomes

```yaml
    image: "nginx:1.31-alpine"   # keep this comment and the quotes
```

If a byte range no longer holds the text the parse said it held, the update
fails instead of writing. A rewrite that has drifted from the parse is a bug,
and corrupting the file would hide it.

## Listing tags

```console
$ docker-devtools registry tags python:3.12-slim
  3.11-slim
* 3.12-slim
  3.13-slim
  3.14-slim
```

Given a tag, the listing keeps only tags sharing its suffix and marks the one
you named, so it answers what that reference could move to. `-alpine` and
`-slim` stay apart for the same reason no policy crosses between them. Pass
`--all` for everything, and `--json` to script against.

The ordering is computed here, not taken from the registry. The OCI
distribution spec requires the tags endpoint to return
"lexical (i.e. case-insensitive alphanumeric order)" and carries no timestamps,
which is the order that puts `3.10` before `3.9`. There is no portable way to
sort by publication date: reading one costs three requests per tag and is
meaningless for reproducible builds, which set it to the epoch. `--sort lexical`
hands the registry's own order back.

Credentials come from `~/.docker/config.json`, including the `credsStore` and
`credHelpers` entries that shell out to `docker-credential-*`, and from
`$DOCKER_CONFIG`, `$REGISTRY_AUTH_FILE` and Podman's `containers/auth.json`.
Behind all of those, `~/.netrc` is consulted, or `$NETRC` when it is set.

## Shell completion

The binary emits a [usage](https://usage.jdx.dev) spec describing its own
command tree, and the `usage` CLI turns that into completions for bash, zsh,
fish, powershell and nushell:

```console
$ mise use usage
$ usage g completion zsh docker-devtools --usage-cmd 'docker-devtools --usage-spec' --install
```

The generated scripts call back to `usage` at completion time, so it has to stay
on your PATH. `mise run completions` regenerates all five, plus a markdown
reference, into `build/`.

## As a Docker CLI plugin

```console
$ docker-devtools install-docker-plugin
$ docker devtools image-refs ls
```

This symlinks the binary into `~/.docker/cli-plugins/`, so upgrading the binary
upgrades the plugin. Windows gets a copy instead, having no dependable
unprivileged symlink. `DOCKER_CONFIG` moves the directory, and `--system`
installs for every user.

The subcommand is `devtools` because Docker validates plugin names against
`^[a-z][a-z0-9]*$` and refuses to load anything else. Python wheels cannot do
this step at install time: they have no post-install hook, and
`~/.docker/cli-plugins/` sits outside every Python install path.

## Python API

The wheel bundles the binary and a typed wrapper.

```python
from docker_devtools import image_ls
from docker_devtools import image_update

for ref in image_ls(".").resolved():
    print(f"{ref.path}:{ref.line}", ref.repository, ref.tag)

report = image_update(".", pin_digest=True, dry_run=True)
for change in report.changes:
    print(change.old, "->", change.new, f"({change.reason})")
```

`image_update` defaults to `dry_run=True`, so calling it by accident cannot
rewrite a repository. The CLI defaults the other way, as a CLI should: `image
update` writes unless you pass `--dry-run`.

The wrapper shells out to the bundled binary, which the wheel installs onto
PATH. Where that directory isn't on PATH, `python -m docker_devtools` runs it
anyway, and `DOCKER_DEVTOOLS_BINARY` points at a specific build.

## Development

`mise.toml` defines the tools and the tasks.

```console
$ mise run build         # compile into ./build
$ mise run test          # go test + pytest
$ mise run lint          # pre-commit across the repo
$ mise run prose         # vale-ai-tells across all markdown
$ mise run conformance   # diff context listing against real docker build
$ mise run completions   # regenerate completions and docs
$ mise run wheels        # every platform wheel into ./dist
$ mise run test-clone    # verify a fresh clone in a container
```

Registry behaviour is tested against `go-containerregistry`'s in-process
registry, so the suite doesn't touch the network or carry recorded fixtures.

## License

MIT. See [LICENSE](LICENSE).

`src/docker_devtools/_find.py` adapts the binary-discovery search order from
[uv](https://github.com/astral-sh/uv), which is MIT OR Apache-2.0.
