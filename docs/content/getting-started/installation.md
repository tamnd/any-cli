---
title: "Installation"
description: "Install any with go install, from a release, or from source."
weight: 20
---

## With Go

```bash
go install github.com/tamnd/any-cli/cmd/any@latest
```

That puts `any` in `$(go env GOPATH)/bin`, which is `~/go/bin` unless you moved
it. Make sure that directory is on your `PATH`.

## Prebuilt binaries

Every [release](https://github.com/tamnd/any-cli/releases) carries archives for
Linux, macOS, and Windows on amd64 and arm64, plus deb, rpm, and apk packages.

## From source

```bash
git clone https://github.com/tamnd/any-cli
cd any-cli
make build        # produces ./bin/any
./bin/any version
```

## Requirements

`any` scaffolds files on its own. The post-scaffold steps shell out to tools you
likely already have:

- **git** to initialise the repo and add the docs theme submodule.
- **go** to run `go mod tidy` (skip with `--no-tidy`).
- **gh** only when you pass `--remote` to create and push the GitHub repo.
