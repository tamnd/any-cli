---
title: "Introduction"
description: "What any does and how the templates work."
weight: 10
---

`any` turns a bare site name into a complete CLI repository. From `any new
reddit` you get `reddit-cli`: a building, testing, linting, releasing repo with
a command tree, a library package, GoReleaser, the GitHub Actions, and a docs
site. You then write the site-specific commands instead of the boilerplate.

## How a name becomes a repo

`any new reddit` derives everything from `reddit`:

- repository `reddit-cli`, module `github.com/tamnd/reddit-cli`
- binary `reddit`, library package `reddit/`
- container image `ghcr.io/tamnd/reddit`, docs domain `reddit-cli.tamnd.com`

Flags override any piece (`--owner`, `--binary`, `--license`, and so on).

## How the templates work

The boilerplate lives in `scaffold/templates`, embedded into the binary. Each
file is a Go `text/template` rendered with the data for one site. The
delimiters are `<<` and `>>` so the GitHub Actions `${{ ... }}` and GoReleaser
`{{ ... }}` expressions inside the templates pass through untouched. In a file
path, `BINARY` and `LIBPKG` are replaced with the binary and library package
names.

## After scaffolding

`any` initialises git, adds the docs theme submodule, runs `go mod tidy`, and
with `--remote` creates the GitHub repository and pushes. From there it is a
normal repo: build it, fill in `cli/`, and tag a release.
