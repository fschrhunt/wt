# wt

Dumb git worktree helper. Creates detached worktrees, lists them, removes them.

```bash
wt new [--no-fetch] [--base <ref>] <agent>  # run from inside a repo → worktrees/<agent>-<repo>, detached at a fresh base
wt list
wt remove <name|path>  # always forces
```

`wt new` fetches `origin` first, then detaches at `origin/HEAD`, falling back
through `origin/main`, `origin/master`, and local `HEAD`. The base checkout in
`repos/` never moves. A failed fetch (offline, no remote) falls back to local
refs with a warning. `--no-fetch` skips the fetch, `--base <ref>` pins the ref
and skips the fetch.

Worktrees start detached, so name a branch before you push:

```bash
git switch -c feat/thing
git push -u origin feat/thing
```

The worktree name (`claude-wt`) is only a directory label. It is not a branch
name, so never push a branch called `claude-wt`. If one was already pushed,
push the real branch name, then delete the old remote branch with
`git push origin --delete claude-wt`.

## Config

`$WT_CONFIG`, else `~/.config/wt/settings.json`, else built-in defaults:

```json
{
  "repos": "~/space/code/repos",
  "worktrees": "~/space/code/worktrees"
}
```

Names are `<agent>-<repo>` and stay flat; collisions get `-1`, `-2`.

## Build

```bash
go build -o wt .
```

## Contributing

Use Go 1.26 or newer and run `./x check` before proposing a change. `./x` defaults to
this non-mutating check: Go formatting, vet, tests and a build to `/dev/null`. CI
runs the same command on pull requests and pushes to main. Add focused tests
when changing behavior.

`./x --help` lists targets. `./x fmt --check` checks without writing; `./x fmt`
formats sources. `./x test` and `./x build` forward Go arguments, for example
`./x build -o /tmp/wt .`. Update relevant docs when behavior changes; this repository
has no changelog convention. Use temporary repos and `WT_CONFIG` for manual checks.
