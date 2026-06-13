# any

Scaffold a new `tamnd/*-cli` repository, fully wired.

`any` writes a complete site CLI from one proven template. Every `tamnd/*-cli`
repo shares the same skeleton: a cobra command tree, a pure-Go library package,
GoReleaser cross-builds with packages and a container image, the ci/release/docs
GitHub Actions, a tago documentation site, and the house style. `any new <site>`
writes all of it at once, so a new CLI starts complete instead of accreting
boilerplate by hand.

## Install

```bash
go install github.com/tamnd/any-cli/cmd/any@latest
```

Or grab a prebuilt binary from the [releases](https://github.com/tamnd/any-cli/releases).

## Usage

```bash
any new reddit                       # scaffold ./reddit-cli, builds as-is
any new reddit --remote              # also create tamnd/reddit-cli and push
any new lobsters -d ~/code --binary lob --short "A command line for Lobsters."
```

`any new <site>` derives everything from the bare site name:

| Field | Example (`reddit`) |
|---|---|
| Repository | `reddit-cli` |
| Module | `github.com/tamnd/reddit-cli` |
| Binary | `reddit` |
| Library package | `reddit/` |
| Container image | `ghcr.io/tamnd/reddit` |
| Docs domain | `reddit-cli.tamnd.com` |

Override any of them with flags (`--owner`, `--binary`, `--license`, `--short`,
`--description`, `--author`, `--email`). Run `any new --help` for the full set.

After scaffolding, `any` initialises a git repo, adds the docs theme submodule,
runs `go mod tidy`, and (with `--remote`) creates the GitHub repository with
`gh` and pushes. Skip steps with `--no-git`, `--no-tidy`.

## What you get

A repository that builds, tests, lints, and releases with no further setup:

```
cmd/<bin>/           thin main, wires cli.Root into fang
cli/                 cobra command tree with version
<site>/              library: a paced, retrying HTTP client and your models
docs/                tago site, deploys to GitHub Pages and Cloudflare
.github/workflows/   ci (build/test/lint/vuln/tidy), release, docs
.goreleaser.yaml     archives, deb/rpm/apk, GHCR image, cosign, SBOMs, taps
Dockerfile Makefile .golangci.yml LICENSE CLAUDE.md
```

Then you write the site-specific commands in `cli/` on top of the library, and
cut the first release with `git tag v0.1.0 && git push --tags`.

## Development

```bash
make build      # ./bin/any
make test       # go test ./...
```

The templates live in `scaffold/templates`, embedded into the binary. Each file
is a Go `text/template` with `<<` `>>` delimiters (so GitHub Actions `${{ }}`
and GoReleaser `{{ }}` expressions pass through), and `BINARY` / `LIBPKG` in a
path are replaced with the binary and library package names.

## License

Apache-2.0. See [LICENSE](LICENSE).
