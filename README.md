# docker-devtools

Work on the Docker files in a repository: the build context a Dockerfile would
send, and the image references it and your Compose files point at.

```console
$ docker-devtools image ls
Dockerfile:1        python:3.11-slim
compose.yaml:3      nginx:1.25-alpine

$ docker-devtools image update --tag-policy same-pattern --dry-run
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
| List a build context | [`docker-build-context`](https://github.com/FlavioAmurrioCS/docker-build-context) |

The build context half is a separate tool with its own conformance suite, which
diffs its output against a real `docker build` for every fixture. This repo
imports that package rather than reimplementing it.

## Install

```console
$ uvx docker-devtools image ls          # no install
$ pipx run docker-devtools image ls     # no install
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

## Usage

```text
docker-devtools context ls [PATH]        list the files Docker would send
docker-devtools context explain PATH     show which .dockerignore rule decided a path
docker-devtools image ls [PATH...]       list every image reference, with file and line
docker-devtools image update [PATH...]   rewrite references in place
docker-devtools install-docker-plugin    register as "docker devtools"
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
| `3.12-slim` | `3.13-slim` | `3.13-slim` | no change | `4.0-slim` |
| `3.12.1-slim` | `3.12.7-slim` | `3.13.0-slim` | `3.12.7-slim` | `4.0-slim` |
| `latest` | no change | no change | no change | no change |

No policy ever changes the suffix: `-alpine` and `-slim` are different images,
and swapping them would change your base distribution without saying so. Tags
with no version, such as `latest` or `bookworm`, are never moved, because there
is no ordering to move along.

Add `--dry-run` to see the plan without writing, and `--fail-on-diff` to exit
non-zero when anything would change, which is what makes it useful in CI.

### What it will not touch

Some references cannot be resolved to an image, and those are reported rather
than guessed at. Pass `--unresolved` to `image ls` to see them:

- `FROM builder`, where `builder` is an earlier stage
- `COPY --from=0`, which indexes a stage
- `FROM $BASE`, which depends on a build argument
- `FROM scratch`, which is the empty base rather than a registry image
- Compose values built from variables, such as `${REGISTRY}/app:latest`

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
$ docker devtools image ls
```

This symlinks the binary into `~/.docker/cli-plugins/`. Use `--system` to
install it for every user.

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
rewrite a repository.

## Development

`mise.toml` defines the tools and the tasks.

```console
$ mise run build         # compile into ./build
$ mise run test          # go test + pytest
$ mise run lint          # pre-commit across the repo
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
