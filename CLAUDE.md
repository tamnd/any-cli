# any-cli

`any` scaffolds a new `tamnd/*-cli` repository from one embedded template tree.
It is the source of truth for the shared boilerplate across every site CLI.

## Layout

- `cmd/any/` thin main, wires `cli.Root()` into charmbracelet/fang.
- `cli/` the cobra command tree: `new` and `version`.
- `scaffold/` the engine. `scaffold.go` walks `templates/`, renders each file,
  and writes it. `repo.go` runs the post-scaffold git, tidy, and gh steps.
- `scaffold/templates/` the embedded template tree, one file per output file.

## How the templates work

- Delimiters are `<<` and `>>`, chosen so GitHub Actions `${{ ... }}` and
  GoReleaser `{{ ... }}` expressions in the templates pass through untouched.
- In a path, `BINARY` becomes the binary name and `LIBPKG` becomes the library
  package name. The `.tmpl` suffix is dropped on output.
- `Site` in `scaffold.go` is the data every template is rendered with. Add a
  field there to expose new substitutions.
- Rendering uses `missingkey=error`, so a typo'd `<< .Feild >>` fails the
  scaffold instead of writing a half-rendered file.

## Keeping templates in step with the fleet

When the shared boilerplate changes in a released CLI (a new GoReleaser option,
a workflow bump), update the matching template here so the next scaffold carries
it. The templates mirror the proven ccrawl-cli setup.

## Style and workflow

House voice in all text: plain, direct, no em-dashes or en-dashes, no "X is not
Y, it is Z" antithesis, no marketing vocabulary. Work on a feature branch and
open a PR; never commit to main. Small, focused commits.

## Verifying a change

A template change is correct when a fresh scaffold still builds clean:

```bash
go run ./cmd/any new probe -d /tmp --no-git --no-tidy
cd /tmp/probe-cli && go mod tidy && go build ./... && go vet ./... && go test ./...
goreleaser check
```
