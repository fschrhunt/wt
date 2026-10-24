package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initRepo creates a git repo with one commit on main and returns its path.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@test")
	run("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f")
	run("commit", "-m", "init")
	return dir
}

// TestResolveBasePrefersFirstExisting pins candidate ordering.
func TestResolveBasePrefersFirstExisting(t *testing.T) {
	dir := initRepo(t)
	cmd := exec.Command("git", "branch", "foo")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git branch foo: %v\n%s", err, out)
	}
	got, err := resolveBase(dir, []string{"missing", "foo", "HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "foo" {
		t.Fatalf("got %q, want foo", got)
	}
}

// TestResolveBaseFallsBackToHEAD pins the offline fallback.
func TestResolveBaseFallsBackToHEAD(t *testing.T) {
	dir := initRepo(t)
	got, err := resolveBase(dir, []string{"missing", "HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "HEAD" {
		t.Fatalf("got %q, want HEAD", got)
	}
}

// TestResolveBaseErrorsWithoutMatch pins the empty-repo error path.
func TestResolveBaseErrorsWithoutMatch(t *testing.T) {
	dir := initRepo(t)
	if _, err := resolveBase(dir, []string{"missing"}); err == nil {
		t.Fatal("expected error for unresolvable refs")
	}
}

// TestNewNamesWorktreeAgentRepo pins the <agent>-<repo> naming contract.
func TestNewNamesWorktreeAgentRepo(t *testing.T) {
	repo := initRepo(t)
	t.Chdir(repo)
	worktrees := t.TempDir()
	cmdNew(t.TempDir(), worktrees, "claude", "", true)
	want := filepath.Join(worktrees, "claude-"+filepath.Base(repo))
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("worktree not created at %s: %v", want, err)
	}
}
