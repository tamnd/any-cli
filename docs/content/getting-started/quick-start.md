---
title: "Quick start"
description: "Scaffold your first CLI and cut a release."
weight: 30
---

Scaffold a new repository:

```bash
any new reddit --short "A command line for Reddit."
```

That writes `./reddit-cli`, initialises git, adds the docs theme, and tidies
modules. Confirm it builds:

```bash
cd reddit-cli
make build && ./bin/reddit version
```

Write your first command in `cli/` on top of the `reddit/` library package, then
publish the repo:

```bash
gh repo create tamnd/reddit-cli --public --source=. --remote=origin --push
```

Or do both at once with `--remote`:

```bash
any new reddit --remote
```

Cut the first release whenever you are ready:

```bash
git tag v0.1.0 && git push --tags
```

GoReleaser builds the archives, Linux packages, the multi-arch GHCR image,
checksums, SBOMs, and a cosign signature. The Homebrew and Scoop steps stay
quiet until their tokens exist.
