---
title: "any"
description: "Scaffold a new tamnd/*-cli repository, fully wired: command tree, library, GoReleaser, GitHub Actions, and a docs site in one command."
heroTitle: "A new CLI, fully wired, in one command"
heroLead: "any writes a complete site CLI from one proven template: a cobra command tree, a pure-Go library, GoReleaser cross-builds with packages and a container image, the ci/release/docs workflows, and a docs site. A new repo starts complete instead of accreting boilerplate by hand."
heroPrimaryURL: "/getting-started/quick-start/"
heroPrimaryText: "Get started"
---

Every `tamnd/*-cli` repo shares the same skeleton. `any` is where that skeleton
lives, so a new CLI starts from a building, releasable repository.

```bash
any new reddit            # scaffold ./reddit-cli
any new reddit --remote   # also create tamnd/reddit-cli and push
```

## What it writes

- A **command tree** (`cli`) with `version`, ready for your subcommands.
- A **library package** with a paced, retrying HTTP client and room for your
  data models.
- **GoReleaser**: archives, deb/rpm/apk, a multi-arch GHCR image, checksums,
  cosign signatures, SBOMs, and Homebrew/Scoop entries that self-disable until
  their tokens exist.
- The **ci, release, and docs** GitHub Actions.
- A **tago docs site** that deploys to GitHub Pages and Cloudflare.

## Where to go next

- [Introduction](/getting-started/introduction/) for how it works.
- [Quick start](/getting-started/quick-start/) to scaffold your first repo.
- [CLI reference](/reference/cli/) for every flag.
