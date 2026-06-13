---
title: "CLI"
description: "Every command and flag."
weight: 10
---

```
any <command> [flags]
```

## Commands

| Command | What it does |
|---|---|
| `new <site>` | Scaffold a new `<site>-cli` repository |
| `version` | Print the version and exit |

## new

```
any new <site> [flags]
```

Scaffolds `<site>-cli` into the current directory (or `--dir`), initialises git,
adds the docs theme submodule, runs `go mod tidy`, and optionally creates and
pushes the GitHub repo.

| Flag | Default | Meaning |
|---|---|---|
| `-d, --dir` | current directory | Parent directory for the new repo |
| `--owner` | `tamnd` | GitHub owner |
| `--binary` | the site name | Command/binary name |
| `-s, --short` | generated | One-line description |
| `--description` | the short one | Longer description |
| `--license` | `Apache-2.0` | SPDX license id |
| `--author` | `Duc-Tam Nguyen` | Copyright holder |
| `--email` | `tamnd87@gmail.com` | Maintainer email |
| `--no-git` | off | Skip git init and the docs theme submodule |
| `--no-tidy` | off | Skip `go mod tidy` |
| `--remote` | off | Create the GitHub repo with `gh` and push |
| `--private` | off | With `--remote`, create the repo private |

The license file is Apache-2.0 text. If you set `--license` to something else,
replace `LICENSE` to match.
