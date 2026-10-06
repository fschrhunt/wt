// Command wt creates detached git worktrees, lists them, and force-removes them.
//
// Layout comes from a three-key JSON config (repos, worktrees, pattern) so any
// machine can point it at its own directories. Everything else is hardcoded:
// worktrees start detached, collisions get a -1/-2 suffix, removal always
// forces. Run wt help for usage.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// fileConfig mirrors settings.json. Empty fields keep their defaults.
type fileConfig struct {
	Repos     string `json:"repos"`
	Worktrees string `json:"worktrees"`
	Pattern   string `json:"pattern"`
}

// worktree pairs a base repo name with one of its checkout paths.
type worktree struct {
	repo string
	path string
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

// validName reports whether s is a single safe path component.
func validName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
		if !ok {
			return false
		}
		if i == 0 && !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// expandTilde resolves a leading ~ to the user's home directory.
func expandTilde(s string) string {
	if s == "~" || strings.HasPrefix(s, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			home = os.Getenv("HOME")
		}
		if s == "~" {
			return home
		}
		return filepath.Join(home, strings.TrimPrefix(s, "~/"))
	}
	return s
}

// defaultConfigPath is used unless WT_CONFIG points elsewhere.
func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".config", "wt", "settings.json")
}

// loadConfig returns the effective repos dir, worktrees dir, and name pattern.
func loadConfig() (repos, worktrees, pattern string) {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = os.Getenv("HOME")
	}
	repos = filepath.Join(home, "space", "code", "repos")
	worktrees = filepath.Join(home, "space", "code", "worktrees")
	pattern = "{task}-{repo}"

	path := os.Getenv("WT_CONFIG")
	if path == "" {
		path = defaultConfigPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return repos, worktrees, pattern
		}
		die("cannot read config: %v", err)
	}
	var cfg fileConfig
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &cfg); err != nil {
			die("cannot parse config: %v", err)
		}
	}
	if cfg.Repos != "" {
		repos = expandTilde(cfg.Repos)
	}
	if cfg.Worktrees != "" {
		worktrees = expandTilde(cfg.Worktrees)
	}
	if cfg.Pattern != "" {
		pattern = cfg.Pattern
	}
	for _, p := range []string{repos, worktrees} {
		if strings.ContainsAny(p, "\n\r\t") {
			die("config paths contain control characters")
		}
		if !filepath.IsAbs(p) || p == "/" {
			die("config repos and worktrees must be absolute paths")
		}
	}
	if pattern == "" {
		die("config pattern must not be empty")
	}
	return repos, worktrees, pattern
}

// applyPattern substitutes the {task} and {repo} placeholders.
func applyPattern(pattern, task, repo string) string {
	out := strings.ReplaceAll(pattern, "{task}", task)
	return strings.ReplaceAll(out, "{repo}", repo)
}

// gitTop returns the physical top-level directory of the current repository.
func gitTop() string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		die("run from inside a git repo")
	}
	top := strings.TrimSpace(string(out))
	resolved, err := filepath.EvalSymlinks(top)
	if err != nil {
		die("cannot resolve repository")
	}
	return resolved
}

// discover lists worktrees under worktreesDir across every base clone in reposDir.
func discover(reposDir, worktreesDir string) []worktree {
	var found []worktree
	entries, err := os.ReadDir(reposDir)
	if err != nil {
		return nil
	}
	root, err := filepath.EvalSymlinks(worktreesDir)
	if err != nil {
		root, err = filepath.Abs(worktreesDir)
		if err != nil {
			return nil
		}
	}
	prefix := root + string(os.PathSeparator)
	for _, e := range entries {
		if !e.IsDir() || !validName(e.Name()) {
			continue
		}
		base := filepath.Join(reposDir, e.Name())
		if st, err := os.Stat(filepath.Join(base, ".git")); err != nil || !st.IsDir() {
			continue
		}
		out, err := exec.Command("git", "-C", base, "worktree", "list", "--porcelain", "-z").Output()
		if err != nil {
			continue
		}
		for _, record := range strings.Split(string(out), "\x00") {
			record = strings.TrimRight(record, "\n")
			path, ok := strings.CutPrefix(record, "worktree ")
			if !ok {
				continue
			}
			path = strings.TrimSpace(path)
			if !strings.HasPrefix(path, prefix) || !validName(filepath.Base(path)) {
				continue
			}
			found = append(found, worktree{repo: e.Name(), path: path})
		}
	}
	return found
}

// cmdNew creates a detached worktree at HEAD for the given task name.
func cmdNew(reposDir, worktreesDir, pattern, task string) {
	if !validName(task) {
		die("invalid task name")
	}
	top := gitTop()
	repo := filepath.Base(top)
	if !validName(repo) {
		die("repository directory name is not usable")
	}
	if err := exec.Command("git", "-C", top, "rev-parse", "--verify", "HEAD").Run(); err != nil {
		die("commit once before creating a worktree")
	}
	name := applyPattern(pattern, task, repo)
	if !validName(name) {
		die("pattern produced an invalid worktree name")
	}
	dest := filepath.Join(worktreesDir, name)
	for i := 1; ; i++ {
		if _, err := os.Lstat(dest); os.IsNotExist(err) {
			break
		}
		dest = fmt.Sprintf("%s%c%s-%d", worktreesDir, os.PathSeparator, name, i)
	}
	if err := os.MkdirAll(worktreesDir, 0o755); err != nil {
		die("cannot create worktrees directory")
	}
	cmd := exec.Command("git", "-C", top, "worktree", "add", "--detach", dest, "HEAD")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		die("could not create worktree")
	}
	fmt.Println(dest)
}

// cmdList prints managed worktree names and paths, sorted by path.
func cmdList(reposDir, worktreesDir string) {
	found := discover(reposDir, worktreesDir)
	if len(found) == 0 {
		fmt.Println("(none)")
		return
	}
	sort.Slice(found, func(i, j int) bool { return found[i].path < found[j].path })
	for _, w := range found {
		fmt.Printf("%s\t%s\n", filepath.Base(w.path), w.path)
	}
}

// cmdRemove force-removes the worktree matching a name or exact path.
func cmdRemove(reposDir, worktreesDir, selector string) {
	if st, err := os.Stat(selector); err == nil && st.IsDir() {
		if resolved, err := filepath.EvalSymlinks(selector); err == nil {
			selector = resolved
		} else if abs, err := filepath.Abs(selector); err == nil {
			selector = abs
		}
	}
	var repo, path string
	for _, w := range discover(reposDir, worktreesDir) {
		if selector == filepath.Base(w.path) || selector == w.path {
			if path != "" {
				die("ambiguous name; pass the exact path")
			}
			repo, path = w.repo, w.path
		}
	}
	if path == "" {
		die("no worktree matches that name or path")
	}
	cmd := exec.Command("git", "-C", filepath.Join(reposDir, repo), "worktree", "remove", "--force", path)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		die("could not remove worktree")
	}
	fmt.Printf("removed: %s\n", path)
}

// cmdHelp prints usage and the config file contract.
func cmdHelp() {
	fmt.Print(`wt — dumb git worktree helper

  wt new <task>          Create a detached worktree at HEAD (run from inside a repo)
  wt list                Show worktrees under the configured worktrees directory
  wt remove <name|path>  Force-remove a worktree
  wt help                Show this help

Config: $WT_CONFIG, else ~/.config/wt/settings.json, else built-in defaults.
  {"repos": "~/space/code/repos", "worktrees": "~/space/code/worktrees",
   "pattern": "{task}-{repo}"}
Worktrees start detached; name a branch before you push:
  git switch -c feat/thing
`)
}

func main() {
	if _, err := exec.LookPath("git"); err != nil {
		die("Git is required")
	}
	reposDir, worktreesDir, pattern := loadConfig()
	args := os.Args[1:]
	name := "help"
	if len(args) > 0 {
		name, args = args[0], args[1:]
	}
	switch name {
	case "help", "-h", "--help":
		if len(args) != 0 {
			die("usage: wt help")
		}
		cmdHelp()
	case "new":
		if len(args) != 1 {
			die("usage: wt new <task> (run from inside a git repo)")
		}
		cmdNew(reposDir, worktreesDir, pattern, args[0])
	case "list":
		if len(args) != 0 {
			die("usage: wt list")
		}
		cmdList(reposDir, worktreesDir)
	case "remove":
		if len(args) != 1 {
			die("usage: wt remove <name | path>")
		}
		cmdRemove(reposDir, worktreesDir, args[0])
	default:
		die("unknown command: %s (try wt help)", name)
	}
}
