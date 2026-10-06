# wt

Dumb git worktree helper. Creates detached worktrees, lists them, removes them.

```bash
wt new <task>          # run from inside a repo → worktrees/<task>-<repo> at HEAD, detached
wt list
wt remove <name|path>  # always forces
```

Worktrees start detached, so name a branch before you push:

```bash
git switch -c feat/thing
```

## Config

`$WT_CONFIG`, else `~/.config/wt/settings.json`, else built-in defaults:

```json
{
  "repos": "~/space/code/repos",
  "worktrees": "~/space/code/worktrees",
  "pattern": "{task}-{repo}"
}
```

Only `{task}` and `{repo}` placeholders. Names stay flat; collisions get `-1`, `-2`.

## Build

```bash
go build -o wt .
```

## Contributing

Use Go 1.26 or newer and run `./x check` before proposing a change. `./x` defaults to
this non-mutating check: Go formatting, vet, tests and a build to `/dev/null`. CI
runs the same command on pull requests and pushes to main. There
are currently no Go tests; add focused tests when changing behavior.

`./x --help` lists targets. `./x fmt --check` checks without writing; `./x fmt`
formats sources. `./x test` and `./x build` forward Go arguments, for example
`./x build -o /tmp/wt .`. Update relevant docs when behavior changes; this repository
has no changelog convention. Use temporary repos and `WT_CONFIG` for manual checks.
