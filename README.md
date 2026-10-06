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
