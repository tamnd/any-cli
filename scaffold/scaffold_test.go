package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewSiteDerivesFields(t *testing.T) {
	s := NewSite("Reddit", Options{})
	cases := map[string]string{
		s.Name:      "reddit",
		s.Repo:      "reddit-cli",
		s.Binary:    "reddit",
		s.LibPkg:    "reddit",
		s.Scheme:    "reddit",
		s.Host:      "reddit.com",
		s.EnvPrefix: "REDDIT",
		s.Module:    "github.com/tamnd/reddit-cli",
		s.Image:     "ghcr.io/tamnd/reddit",
		s.Domain:    "reddit-cli.tamnd.com",
		s.GitHubURL: "https://github.com/tamnd/reddit-cli",
		s.License:   "Apache-2.0",
		s.Email:     "tamnd87@gmail.com",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestNewSiteOverrides(t *testing.T) {
	s := NewSite("lobsters", Options{Owner: "acme", Binary: "lob", License: "MIT"})
	if s.Module != "github.com/acme/lobsters-cli" {
		t.Errorf("Module = %q", s.Module)
	}
	if s.Binary != "lob" || s.Image != "ghcr.io/acme/lob" {
		t.Errorf("Binary/Image = %q / %q", s.Binary, s.Image)
	}
	if s.EnvPrefix != "LOB" {
		t.Errorf("EnvPrefix = %q, want LOB (the upper-cased binary)", s.EnvPrefix)
	}
	if s.License != "MIT" {
		t.Errorf("License = %q", s.License)
	}
}

func TestRenderWritesBuildableTree(t *testing.T) {
	dest := t.TempDir()
	site := NewSite("reddit", Options{Short: "A command line for Reddit."})

	files, err := Render(dest, site)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 25 {
		t.Fatalf("wrote only %d files, expected the full skeleton", len(files))
	}

	// Path tokens must be substituted, never left literal.
	for _, f := range files {
		if strings.Contains(f, "BINARY") || strings.Contains(f, "LIBPKG") {
			t.Errorf("path token left unsubstituted: %s", f)
		}
		if strings.HasSuffix(f, ".tmpl") {
			t.Errorf("output kept .tmpl suffix: %s", f)
		}
	}

	// Spot-check files that prove the path tokens resolved.
	for _, want := range []string{
		"go.mod",
		".goreleaser.yaml",
		".github/workflows/release.yml",
		"cmd/reddit/main.go",
		"reddit/reddit.go",
		"reddit/reddit_test.go",
		"reddit/domain.go",
		"reddit/domain_test.go",
		"docs/tago.toml",
		"docs/content/reference/output.md",
		"docs/content/reference/troubleshooting.md",
	} {
		if _, err := os.Stat(filepath.Join(dest, want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}

	// The module path must reach go.mod.
	mod, err := os.ReadFile(filepath.Join(dest, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mod), "module github.com/tamnd/reddit-cli") {
		t.Errorf("go.mod module line wrong:\n%s", mod)
	}

	// The domain driver must register the site's scheme so a host can mount it.
	dom, err := os.ReadFile(filepath.Join(dest, "reddit/domain.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dom), `Scheme: "reddit"`) {
		t.Errorf("domain.go missing the reddit scheme:\n%s", dom)
	}

	// No unresolved template delimiters should survive anywhere.
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(dest, f))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "<<") || strings.Contains(string(b), ">>") {
			t.Errorf("%s still contains template delimiters", f)
		}
	}
}

func TestRenderRefusesToOverwrite(t *testing.T) {
	dest := t.TempDir()
	site := NewSite("reddit", Options{})
	if _, err := Render(dest, site); err != nil {
		t.Fatal(err)
	}
	if _, err := Render(dest, site); err == nil {
		t.Error("second render into the same dir should fail, not clobber")
	}
}
