// Package scaffold renders the embedded template tree into a new site CLI
// repository. Every file under templates/ is a Go text/template using the
// delimiters << and >> (chosen so GitHub Actions ${{ ... }} and GoReleaser
// {{ ... }} expressions in the templates pass through untouched), and its path
// carries two replaceable tokens: BINARY for the command name and LIBPKG for
// the library package name.
package scaffold

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

//go:embed all:templates
var templates embed.FS

// Site is the data a single template render is given.
type Site struct {
	Name        string // bare site name, e.g. "reddit"
	Repo        string // repository name, e.g. "reddit-cli"
	Binary      string // command/binary name, e.g. "reddit"
	LibPkg      string // library package name, e.g. "reddit"
	Owner       string // GitHub owner, e.g. "tamnd"
	Module      string // Go module path, e.g. "github.com/tamnd/reddit-cli"
	Image       string // container image, e.g. "ghcr.io/tamnd/reddit"
	Domain      string // docs domain, e.g. "reddit-cli.tamnd.com"
	PagesURL    string // GitHub Pages URL, e.g. "https://tamnd.github.io/reddit-cli/"
	GitHubURL   string // repo URL, e.g. "https://github.com/tamnd/reddit-cli"
	Short       string // one-line description
	Description string // longer description
	License     string // SPDX id, e.g. "Apache-2.0"
	Author      string // copyright holder
	Email       string // maintainer email
	Year        int    // copyright year
}

// NewSite fills a Site from a bare name and the chosen options, deriving every
// path-shaped field so a caller only sets what actually varies.
func NewSite(name string, opt Options) Site {
	name = strings.ToLower(strings.TrimSpace(name))
	owner := orDefault(opt.Owner, "tamnd")
	binary := orDefault(opt.Binary, name)
	repo := name + "-cli"
	short := orDefault(opt.Short, fmt.Sprintf("A command line for %s.", name))
	return Site{
		Name:        name,
		Repo:        repo,
		Binary:      binary,
		LibPkg:      name,
		Owner:       owner,
		Module:      fmt.Sprintf("github.com/%s/%s", owner, repo),
		Image:       fmt.Sprintf("ghcr.io/%s/%s", owner, binary),
		Domain:      fmt.Sprintf("%s.%s", repo, "tamnd.com"),
		PagesURL:    fmt.Sprintf("https://%s.github.io/%s/", owner, repo),
		GitHubURL:   fmt.Sprintf("https://github.com/%s/%s", owner, repo),
		Short:       short,
		Description: orDefault(opt.Description, short),
		License:     orDefault(opt.License, "Apache-2.0"),
		Author:      orDefault(opt.Author, "Duc-Tam Nguyen"),
		Email:       orDefault(opt.Email, "tamnd87@gmail.com"),
		Year:        opt.Year,
	}
}

// Options carries the parts of a Site a caller may override; zero values fall
// back to the house defaults in NewSite.
type Options struct {
	Owner       string
	Binary      string
	Short       string
	Description string
	License     string
	Author      string
	Email       string
	Year        int
}

// Render writes every template into destDir and returns the relative paths it
// wrote, in walk order. It never overwrites an existing file; a collision is an
// error so a half-finished scaffold is never silently clobbered.
func Render(destDir string, site Site) ([]string, error) {
	if site.Year == 0 {
		site.Year = time.Now().Year()
	}
	var written []string
	err := fs.WalkDir(templates, "templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := relPath(strings.TrimPrefix(p, "templates/"), site)
		out := filepath.Join(destDir, rel)

		if _, err := os.Stat(out); err == nil {
			return fmt.Errorf("refusing to overwrite %s", rel)
		}

		raw, err := templates.ReadFile(p)
		if err != nil {
			return err
		}
		body, err := renderString(rel, string(raw), site)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(out, []byte(body), fileMode(rel)); err != nil {
			return err
		}
		written = append(written, rel)
		return nil
	})
	return written, err
}

// relPath turns a template path into its output path: the .tmpl suffix is
// dropped and the BINARY / LIBPKG path tokens are substituted.
func relPath(rel string, site Site) string {
	rel = strings.TrimSuffix(rel, ".tmpl")
	rel = strings.ReplaceAll(rel, "BINARY", site.Binary)
	rel = strings.ReplaceAll(rel, "LIBPKG", site.LibPkg)
	return rel
}

func renderString(name, body string, site Site) (string, error) {
	t, err := template.New(name).Delims("<<", ">>").Option("missingkey=error").Parse(body)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, site); err != nil {
		return "", err
	}
	return b.String(), nil
}

// fileMode keeps scripts executable and everything else a plain file.
func fileMode(rel string) os.FileMode {
	if strings.HasSuffix(rel, ".py") || strings.HasSuffix(rel, ".sh") {
		return 0o755
	}
	return 0o644
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
