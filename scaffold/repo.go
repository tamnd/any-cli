package scaffold

import (
	"fmt"
	"io"
	"os/exec"
)

// ThemeSubmodule is the docs theme every site shares, added as a git submodule
// at docs/themes/tago-doks so the docs workflow can build the tago site.
const ThemeSubmodule = "https://github.com/tamnd/tago-doks"

// run executes a command in dir, streaming its output to w, and returns an
// error that names the command on failure.
func run(w io.Writer, dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %v: %w", name, args, err)
	}
	return nil
}

// have reports whether a binary is on PATH.
func have(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// InitGit turns destDir into a git repo on main, adds the docs theme submodule,
// and makes the first commit. The submodule and commit are best-effort so a
// machine without network still gets a valid local repo.
func InitGit(w io.Writer, destDir, message string) error {
	if !have("git") {
		return fmt.Errorf("git not found on PATH")
	}
	if err := run(w, destDir, "git", "init", "-q", "-b", "main"); err != nil {
		return err
	}
	if err := run(w, destDir, "git", "submodule", "add", "-q",
		ThemeSubmodule, "docs/themes/tago-doks"); err != nil {
		fmt.Fprintf(w, "note: docs theme submodule not added (%v); add it later with:\n"+
			"  git submodule add %s docs/themes/tago-doks\n", err, ThemeSubmodule)
	}
	if err := run(w, destDir, "git", "add", "-A"); err != nil {
		return err
	}
	if err := run(w, destDir, "git", "commit", "-q", "-m", message); err != nil {
		return err
	}
	return nil
}

// Tidy runs go mod tidy so the new repo has a go.sum. It is best-effort: it
// needs network the first time and the repo's CI tidy job catches any drift.
func Tidy(w io.Writer, destDir string) error {
	if !have("go") {
		return fmt.Errorf("go not found on PATH")
	}
	return run(w, destDir, "go", "mod", "tidy")
}

// CreateRemote creates the GitHub repository with gh and pushes the current
// branch, wiring origin in the process.
func CreateRemote(w io.Writer, destDir, owner, repo string, private bool) error {
	if !have("gh") {
		return fmt.Errorf("gh (GitHub CLI) not found on PATH")
	}
	vis := "--public"
	if private {
		vis = "--private"
	}
	return run(w, destDir, "gh", "repo", "create",
		fmt.Sprintf("%s/%s", owner, repo),
		vis, "--source=.", "--remote=origin", "--push")
}
