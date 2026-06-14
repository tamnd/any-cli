---
name: new-cli
description: >-
  Start a new tamnd/*-cli: a Go single-binary CLI for a website's public data
  (Reddit, Letterboxd, arXiv, and so on). Use when the user wants to build,
  scaffold, or bootstrap a new site CLI in the ccrawl-cli/ytb-cli family. Wraps
  the `any` scaffolder so a new repo starts complete, then guides the site work.
---

# Building a new tamnd/*-cli

A site CLI is one Go binary that reads a website's public data without a paid
API key and prints clean, pipeable records. The fleet (ccrawl-cli, ytb-cli,
x-cli, bilibili-cli, goodread-cli, facebook-cli, amz-cli, archive-cli,
wikipedia-cli) all share one skeleton. Do not rebuild that skeleton by hand: the
`any` scaffolder writes it.

## 1. Scaffold with `any`

`any` lives at `github.com/tamnd/any-cli`. Use it, building it first if needed:

```bash
# from a clone of tamnd/any-cli
go run ./cmd/any new <site> --short "A command line for <Site>."
# or, once installed:
any new <site> --remote        # also creates and pushes the GitHub repo
```

This writes `<site>-cli`, built on the shared `kit` framework
(`github.com/tamnd/any-cli/kit`):

- `<site>/<site>.go` — a paced, retrying HTTP client plus an example `Page`
  record with `kit` struct tags.
- `<site>/domain.go` — a `kit.Domain` driver, the single source of truth. It
  registers the operations once. The same definition is both the binary's
  command tree and a `database/sql`-style URI driver: a host like
  [ant](https://github.com/tamnd/ant) blank-imports the package and dereferences
  `<scheme>://` URIs through it. There is no second implementation to keep in
  step.
- `<site>/domain_test.go` — offline tests for the driver wiring (Classify,
  Locate, mint).
- `cli/` — assembles a `kit.App` from the domain and runs it; `kit.Run` auto-adds
  `serve` (HTTP/NDJSON) and `mcp` (stdio) subcommands, and fang gives
  `--version`, help, and completion.
- `cmd/<bin>` entry point, `.goreleaser.yaml`, ci/release/docs workflows, a tago
  docs site, `.golangci.yml`, Dockerfile, Makefile, and LICENSE.

It initialises git, adds the docs theme submodule, and runs `go mod tidy`. The
result builds, serves, and releases as-is, with one live example operation.

Useful flags: `--owner`, `--binary`, `--host` (the site host the client and URI
driver target, default `<site>.com`), `--license` (default Apache-2.0; use
AGPL-3.0 only when the work derives from AGPL source, and then replace the
Apache LICENSE file), `--dir`, `--no-git`, `--no-tidy`, `--remote`, `--private`.

## 2. Write the spec first

Before implementing, write one long spec (real substance, not a stub) at
`~/notes/Spec/NNNN/NNNN_<site>.md` covering the data model, the public endpoint
map, the command surface, and distribution. Site CLI specs live in the 8000s;
use the next free number.

## 3. Implement the site

- Reverse-engineer the public endpoints (internal JSON APIs, `.json` views,
  RSS, JSON-LD, sitemaps). No paid API key. Stay polite with the client's
  pacing and retries, and set an honest User-Agent.
- Model every useful surface as a clean record in `<site>/<site>.go`, with a
  client method that returns it. Tag the fields: `kit:"id"` for the URI id,
  `kit:"body"` for the prose that `cat` and the Markdown export print,
  `kit:"link,kind=<scheme>/<type>"` for edges a host can follow.
- Declare each operation once in `<site>/domain.go` with `kit.Handle`. That one
  call gives you the command, the `serve` route, the `mcp` tool, and the
  `ant get <scheme>://...` dereference from the same handler. Replace the example
  `page`/`links` ops with the real ones.
- Mark the canonical one-record fetch `Single: true, Resolver: true` with a
  `URIType`; mark a member-lister `List: true`. A list op must emit records that
  are themselves addressable (often a stub of a resolver type) so every member
  is a URI a host can follow. A list op can only emit a type some resolver op
  also mints, so seed the type with a resolver op first.
- Output formats are kit's, shared by every operation:
  `-o auto|table|json|jsonl|csv|tsv|url|raw`, plus `--fields`, `--template`,
  `-n/--limit`. Do not reimplement them.
- No Go `internal/` directories. Reusable parsers go in `pkg/*`.
- Keep it pure-Go and `CGO_ENABLED=0` buildable.

## 4. House rules

- Style in all text (code comments, README, docs, commits, PRs, release notes):
  plain, direct, written by a person. No em-dashes or en-dashes, no "X is not Y,
  it is Z" antithesis, no marketing vocabulary.
- Always work on a feature branch and open a PR. Never commit to main. Small,
  focused commits with a human-voice title and body.
- Commit identity is the default `tamnd87@gmail.com`.

## 5. Release

Push a tag to fan out every artifact:

```bash
git tag v0.1.0 && git push --tags
```

The GHCR image tag has no `v` prefix (`ghcr.io/tamnd/<bin>:0.1.0`). GoReleaser's
`changelog: use: github` produces a plain commit list, so after the run replace
the release body with human notes:

```bash
gh release edit v0.1.0 --notes-file notes.md
```

Add a page under `docs/content/release-notes/` (hyphenated filename like
`v0-1-0.md` so the tago slug is predictable).

## Keeping the scaffold current

When the shared boilerplate improves in a released CLI (a GoReleaser option, a
workflow bump), update the matching template in `tamnd/any-cli` under
`scaffold/templates/` so the next scaffold carries it.
