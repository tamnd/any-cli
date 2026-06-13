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

This writes `<site>-cli`: the `cmd/<bin>` entry point, the `cli/` cobra tree, a
`<site>/` library with a paced retrying HTTP client, `.goreleaser.yaml`, the
ci/release/docs workflows, a tago docs site, `.golangci.yml`, Dockerfile,
Makefile, and LICENSE. It initialises git, adds the docs theme
submodule, and runs `go mod tidy`. The result builds and releases as-is.

Useful flags: `--owner`, `--binary`, `--license` (default Apache-2.0; use
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
- Model every useful surface into a clean record with a JSON shape, emitted as
  JSONL one record per line.
- Build commands in `cli/` on top of the `<site>/` library. Mirror the ccrawl
  command and output conventions (`-o table|json|jsonl|csv|tsv|url|raw`,
  `--fields`, `--template`, `-n`, `-j`).
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
