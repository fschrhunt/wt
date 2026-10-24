// Command wt creates detached git worktrees, lists them, and force-removes them.
//
// Directories come from a two-key JSON config (repos, worktrees) so any machine
// can point it at its own layout. Worktrees are named <agent>-<repo>, start
// detached, get a -1/-2 suffix on collision, and removal always forces. Run
// wt help for usage.
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

// loadConfig returns the effective repos dir and worktrees dir.
func loadConfig() (repos, worktrees string) {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = os.Getenv("HOME")
	}
	repos = filepath.Join(home, "space", "code", "repos")
	worktrees = filepath.Join(home, "space", "code", "worktrees")

	path := os.Getenv("WT_CONFIG")
	if path == "" {
		path = defaultConfigPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return repos, worktrees
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
	for _, p := range []string{repos, worktrees} {
		if strings.ContainsAny(p, "\n\r\t") {
			die("config paths contain control characters")
		}
		if !filepath.IsAbs(p) || p == "/" {
			die("config repos and worktrees must be absolute paths")
		}
	}
	return repos, worktrees
}

// defaultBaseCandidates orders the refs cmdNew tries after fetching.
var defaultBaseCandidates = []string{"origin/HEAD", "origin/main", "origin/master", "HEAD"}

// hasRemote reports whether name is a configured remote.
func hasRemote(dir, name string) bool {
	return exec.Command("git", "-C", dir, "remote", "get-url", name).Run() == nil
}

// fetchRemote updates remote-tracking refs. The caller falls back to local
// refs when it fails (offline, no network), so output goes to stderr.
func fetchRemote(dir, remote string) error {
	cmd := exec.Command("git", "-C", dir, "fetch", remote)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	// Best effort: refresh the origin/HEAD pointer so a moved default
	// branch does not leave new worktrees behind. Ignore failures.
	refresh := exec.Command("git", "-C", dir, "remote", "set-head", remote, "-a")
	refresh.Stdout = os.Stderr
	refresh.Stderr = os.Stderr
	_ = refresh.Run()
	return nil
}

// resolveBase returns the first candidate that verifies in dir.
func resolveBase(dir string, candidates []string) (string, error) {
	for _, ref := range candidates {
		if ref == "" {
			continue
		}
		if err := exec.Command("git", "-C", dir, "rev-parse", "--verify", ref).Run(); err == nil {
			return ref, nil
		}
	}
	return "", fmt.Errorf("no usable base ref")
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

// cmdNew creates a detached worktree named <agent>-<repo> from the repository
// in the current directory. It fetches origin first (unless skipped) and
// detaches at origin/HEAD, falling back through origin/main, origin/master, and
// finally local HEAD, so worktrees start from fresh upstream refs instead of a
// stale base checkout. The base checkout itself never moves. An explicit base
// ref pins the worktree and skips the fetch.
func cmdNew(reposDir, worktreesDir, agent, base string, noFetch bool) {
	if !validName(agent) {
		die("invalid agent name")
	}
	top := gitTop()
	repo := filepath.Base(top)
	if !validName(repo) {
		die("repository directory name is not usable")
	}
	var baseRef string
	if base != "" {
		if err := exec.Command("git", "-C", top, "rev-parse", "--verify", base).Run(); err != nil {
			die("unknown base ref: %s", base)
		}
		baseRef = base
	} else {
		if !noFetch && hasRemote(top, "origin") {
			if err := fetchRemote(top, "origin"); err != nil {
				fmt.Fprintln(os.Stderr, "warning: fetch failed; falling back to local refs")
			}
		}
		ref, err := resolveBase(top, defaultBaseCandidates)
		if err != nil {
			die("commit once before creating a worktree")
		}
		baseRef = ref
		if baseRef == "HEAD" {
			fmt.Fprintln(os.Stderr, "warning: no remote-tracking base found; using local HEAD")
		}
	}
	name := agent + "-" + repo
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
	cmd := exec.Command("git", "-C", top, "worktree", "add", "--detach", dest, baseRef)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		die("could not create worktree")
	}
	fmt.Fprintf(os.Stderr, "based on %s\n", baseRef)
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

  wt new [--no-fetch] [--base <ref>] <agent>  Create <agent>-<repo> detached at a fresh base (run from inside a repo)
  wt list                Show worktrees under the configured worktrees directory
  wt remove <name|path>  Force-remove a worktree
  wt help                Show this help

Config: $WT_CONFIG, else ~/.config/wt/settings.json, else built-in defaults.
  {"repos": "~/space/code/repos", "worktrees": "~/space/code/worktrees"}
New worktrees fetch origin first, then detach at origin/HEAD (falling back
through origin/main, origin/master, and local HEAD). The base checkout never
moves; a failed fetch falls back to local refs with a warning. --no-fetch
skips the fetch, --base pins the ref instead.
Worktrees start detached; name a branch before you push. The worktree name is
a directory label, not a branch name:
  git switch -c feat/thing
  git push -u origin feat/thing
`)
}

// parseNewArgs splits leading --no-fetch/--base flags from the agent name.
func parseNewArgs(args []string) (agent, base string, noFetch bool) {
	rest := args
	for len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
		switch rest[0] {
		case "--no-fetch":
			noFetch = true
			rest = rest[1:]
		case "--base":
			if len(rest) < 2 || rest[1] == "" || strings.HasPrefix(rest[1], "-") ||
				strings.ContainsAny(rest[1], " \t\n\r") {
				die("usage: wt new [--no-fetch] [--base <ref>] <agent> (run from inside a git repo)")
			}
			base = rest[1]
			rest = rest[2:]
		default:
			die("usage: wt new [--no-fetch] [--base <ref>] <agent> (run from inside a git repo)")
		}
	}
	if len(rest) != 1 {
		die("usage: wt new [--no-fetch] [--base <ref>] <agent> (run from inside a git repo)")
	}
	return rest[0], base, noFetch
}

func main() {
	if _, err := exec.LookPath("git"); err != nil {
		die("Git is required")
	}
	reposDir, worktreesDir := loadConfig()
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
		agent, base, noFetch := parseNewArgs(args)
		cmdNew(reposDir, worktreesDir, agent, base, noFetch)
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
