# paddock

**Run Claude Code in Docker Sandboxes with your own `~/.claude` setup — read-only, with untrusted project hooks blocked.**

[Docker Sandboxes](https://docs.docker.com/ai/sandboxes/) (`sbx`) runs Claude
Code in an isolated microVM, so it can work without permission prompts. But
every new sandbox starts bare: no `CLAUDE.md`, no custom agents or slash
commands, no plugins, no git identity, and `gh` isn't logged in. paddock is a
small CLI on top of `sbx` that brings your setup in with one command, and
never lets the sandbox change it. The first time you run it, it walks you
through a one-time setup.

```sh
cd ~/code/my-project
paddock                # Claude Code in this project's sandbox, with your setup
```

Before starting anything, `paddock --dry-run` shows exactly what the sandbox
will get:

![paddock --dry-run showing the sandbox name, the project folder Claude can edit, the read-only config, which host hooks will run, and the plugins](docs/demo.gif)

```
paddock would create a new sandbox for my-project.

  Sandbox   claude-my-project-3f9a1c
  Project   ~/code/my-project  (Claude can edit)
  Config    ~/.claude: CLAUDE.md, agents, commands, settings.json  (read-only)

  Hooks     2 of your 19 host hooks will run. Project hooks never run.
            ✓ SessionStart      ~/.claude/hooks/session-start.sh
            ✓ UserPromptSubmit  echo '{"hookSpecificOutput":{"hookEventName":"Use…
            ✗ 17 blocked (-v to list them)

  Plugins   code-review, gopls-lsp, my-team-tools

Nothing was started. Run paddock without --dry-run to start it.
```

**Jump to:** [Problems it solves](#problems-paddock-solves) ·
[Install](#install) · [Commands](#commands) ·
[Configuration](#configuration) · [What's shared](#whats-shared-and-what-isnt) ·
[How it works](#how-it-works)

## Problems paddock solves

### Claude Code in Docker Sandboxes ignores my `CLAUDE.md`, agents and slash commands

`sbx` doesn't import user-level Claude Code config from `~/.claude`. paddock
mounts the parts you choose read-only and links them into the sandbox's
`~/.claude`, so your instructions, agents and commands work as they do on
your machine.

### I want my settings in the sandbox without the sandbox being able to change them

Everything from `~/.claude` is mounted read-only; an attempt to write there
fails with `Read-only file system`. The sandbox gets its own `settings.json`,
built from the portable parts of yours: model, effort level, `env`, enabled
plugins, plugin marketplaces and `permissions.allow`.

### I don't want a cloned repo's `.claude/settings.json` hooks to run

A repo's hooks are arbitrary shell commands that run when Claude starts,
stops, or uses a tool. paddock installs the host hooks you allow as Claude
Code managed settings with `allowManagedHooksOnly: true`, so project and
plugin hooks are blocked in every paddock sandbox.

### My host hooks call scripts that only exist on my machine

Notification sounds, audit logs and status scripts usually need your host.
Only the hooks you allow are installed. On first run, paddock finds the
hooks that could run in a sandbox and asks about each one. Simple `echo`
hooks default to yes; scripts default to no, because they'll run on Linux
inside the sandbox.

### `gh` fails in the sandbox: `Failed to log in to github.com using token (GH_TOKEN)`

The sandbox has no GitHub token. Save one for every sandbox:

```sh
sbx secret set github --command 'gh auth token'
```

`paddock doctor` checks for this. The same token lets the sandbox install
plugins from private GitHub marketplaces.

### `git commit` in the sandbox fails: `Author identity unknown`

paddock copies your host `user.name` and `user.email` into each new sandbox.

### Two projects with the same folder name share one sandbox

paddock names each sandbox after the folder plus a short hash of its full
path, so `~/work/api` and `~/oss/api` get separate sandboxes.

### I exited the sandbox and want my conversation back

Sandboxes, and the conversations in them, persist until you remove them:

```sh
paddock -- --continue   # resume the last conversation
paddock -- --resume     # pick one
```

### I want several Claude sessions on one repo at once

Give each extra session a name:

```sh
paddock --name fix-login        # a second sandbox for this repo
paddock --name docs             # and a third
```

A named sandbox doesn't edit your folder. It works on its own clone of the
repo, starting from the branch you have checked out, so parallel sessions
never touch the same files. Each named sandbox is a git remote of your repo;
bring its commits back while the sandbox is running:

```sh
git fetch sandbox-claude-my-project-3f9a1c-fix-login
git log main..sandbox-claude-my-project-3f9a1c-fix-login/main   # what it did on main
```

paddock prints the exact `git fetch` command when the sandbox starts and when
you exit. Fetched commits stay in your repo after the sandbox is removed;
commits you haven't fetched, and uncommitted work, are lost with it.
`--name` needs the top folder of the repo's main checkout, not a subfolder
or a `git worktree`. `paddock ls` shows each sandbox's name, and `stop`, `rm`
and `shell` take `--name` too.

### I want Claude to review a PR from someone I don't know

```sh
paddock review https://github.com/OWNER/REPO/pull/123   # or OWNER/REPO#123, or 123 in a clone
```

paddock clones the PR into a fresh folder with git hooks switched off, so
nothing from the PR runs on your machine. Claude reviews it in a throwaway
sandbox where no hooks run at all, not even the host hooks you allowed, and
the sandbox and the clone are deleted when you exit. Add `--keep` to keep
them. It needs the [GitHub CLI](https://cli.github.com) (`gh`), logged in.

## Install

You need [Docker Sandboxes](https://docs.docker.com/ai/sandboxes/) (`sbx`)
installed and signed in.

**Script** (macOS and Linux, amd64 and arm64; installs to `~/.local/bin`, or
`$PADDOCK_INSTALL_DIR`):

```sh
curl -fsSL https://raw.githubusercontent.com/stiyyagura0901/paddock/main/install.sh | sh
```

**Release archive:** download from
[Releases](https://github.com/stiyyagura0901/paddock/releases) and put
`paddock` on your `PATH`.

**Go** (1.24+):

```sh
go install github.com/stiyyagura0901/paddock/cmd/paddock@latest
```

Then run `paddock` in a project folder. The first run asks which of your hooks
should run in sandboxes, saves `~/.config/paddock/config.yaml`, and checks your
setup before starting. You can run the same setup on its own with
`paddock init`, and the checks with `paddock doctor`:

```
✓ sbx           sbx version v0.49.2
✓ config        ~/.config/paddock/config.yaml
✓ host config   ~/.claude
✓ git identity  copied into new sandboxes
✓ ssh agent     keys loaded; forwarded to sandboxes
✓ claude login  saved for all sandboxes
! github token  no global github secret; gh in sandboxes won't be logged in
                fix: sbx secret set github --command 'gh auth token'
✓ skills        host skills imported
```

Every warning comes with the command that fixes it.

## Commands

| Command | What it does |
| --- | --- |
| `paddock [DIR] [-- ARGS]` | Start, or rejoin, the sandbox for DIR (default: current folder). Arguments after `--` go to Claude Code. |
| `paddock --name NAME [DIR] [-- ARGS]` | Start, or rejoin, another sandbox for DIR that works on its own clone of the git repo, so several sessions can run at once. |
| `paddock --dry-run [-v] [--name NAME] [DIR]` | Show what would be shared and which hooks would run, without starting anything. `-v` lists blocked hooks and the `sbx` command. |
| `paddock review [--keep] PR [-- ARGS]` | Have Claude review a GitHub pull request (a URL, `OWNER/REPO#N`, or `N` for this folder's repo) in a throwaway sandbox with no hooks. Arguments after `--` replace the review prompt. Takes `--dry-run`. |
| `paddock init` | Run the first-run setup, or re-check it. Never overwrites an existing config. |
| `paddock doctor` | Check `sbx`, config, git identity, SSH agent, Claude login, GitHub token, skills, and out-of-date sandboxes. |
| `paddock sync` | Copy `~/.claude/skills` into sandboxes after you change them. |
| `paddock ls` | List your paddock sandboxes by project folder and name, with status and when each was last used. |
| `paddock stop`, `shell` `[--name NAME] [DIR]` | Stop a project's sandbox (conversations are kept), or open a shell in it. |
| `paddock rm [-f] [--name NAME] [DIR]` | Remove a project's sandbox and its conversations. Asks first unless you pass `-f`. |

paddock refuses to mount `/` or your home folder; run it in a project folder.

## Configuration

`~/.config/paddock/config.yaml` (or `$XDG_CONFIG_HOME/paddock/config.yaml`, or
the path in `$PADDOCK_CONFIG`):

```yaml
# Entries of ~/.claude mounted read-only and linked into the sandbox.
link:
  - CLAUDE.md
  - agents
  - commands
  - hooks          # needed if an allowed hook runs a script from ~/.claude/hooks

hooks:
  # Host hooks run in the sandbox only if their command starts with one of
  # these prefixes. Project and plugin hooks never run.
  allow:
    - "echo "
    - "~/.claude/hooks/session-start.sh"

plugins:
  # Host plugins to turn off inside the sandbox, as name@marketplace.
  disable: []

# Extra host folders for every sandbox. Add :ro to make one read-only.
mounts:
  - "~/code/shared-lib:ro"

# Environment variables: KEY=VALUE, or just KEY to pass this machine's value.
env:
  - "NODE_ENV=development"
  - "NPM_TOKEN"

# MCP servers registered with sbx mcp add.
mcp:
  - notion

# Additions for one project folder, on top of everything above. Each key
# takes link, hooks, plugins, mounts, env and mcp.
projects:
  ~/code/my-project:
    mounts: ["~/data/fixtures:ro"]
    env: ["DATABASE_URL"]
```

Unknown keys are an error, so a typo can't silently change what runs.
`paddock --dry-run` lists extra mounts, env variable names (never their
values) and MCP servers.

Per-project settings live in your config, never in the repo, so a cloned repo
can't give itself hooks, mounts or env.

Settings apply when a sandbox is created. After you change them, `paddock` and
`paddock doctor` point out sandboxes created with older settings; recreate one
with `paddock rm` then `paddock`. Env entries also apply when you rejoin a
sandbox.

### Sharing a config with your team

Put the settings everyone needs in one file and point each config at it:

```yaml
extends: https://raw.githubusercontent.com/my-org/dev-setup/main/paddock.yaml
```

`extends` takes a local path (relative paths start from your config's folder)
or an https URL. A URL is cached for an hour; if it can't be fetched later,
paddock warns and uses the cached copy. The base's entries come first and
yours are added after them, project by project; for `env`, your value wins
when both set the same key. A base can't extend another base.

A base fetched from a URL can't set `mounts` or `projects`, because the file
can change without you noticing and shouldn't be able to share more of your
machine. Put those in your own config, or use a local file you control.

## What's shared, and what isn't

| From your machine | In the sandbox |
| --- | --- |
| The project folder | Read-write, at the same path (a named sandbox reads it and works on its own clone) |
| `~/.claude` entries in `link:` and `settings.json` | Read-only |
| Folders in `mounts:` | Read-write, or read-only with `:ro` |
| Model, effort, `env`, plugins, marketplaces, `permissions.allow` | Copied into the sandbox's own settings |
| Hooks matching `hooks.allow` | Installed as managed hooks; all others blocked |
| Skills | Copied by `sbx skills import` (`paddock sync`) |
| git name and email; SSH agent | Copied; forwarded |

Your host `~/.claude` is never written to. Things to know:

- Claude can still edit, commit and push the project folder. That's the point
  of the sandbox; use git to review what it did.
- The `env` block of your `settings.json` is copied in. Don't keep secrets
  there that you wouldn't give the sandbox.
- Network access is governed by `sbx` policies (`sbx policy`), not paddock.

## How it works

paddock is a thin wrapper around `sbx run`. For a new sandbox it mounts the
project and your chosen config, and applies a small
[kit](https://docs.docker.com/ai/sandboxes/) embedded in the binary. When the
sandbox starts, the kit:

1. links your config into the sandbox's `~/.claude`;
2. builds the sandbox's `settings.json` from the portable parts of yours;
3. writes `/etc/claude-code/managed-settings.json` with your allowed hooks and
   `allowManagedHooksOnly: true`. Managed settings outrank project and plugin
   settings, which is what blocks every other hook.

paddock records a fingerprint of those settings per sandbox so it can tell you
later when one is out of date. Reattaching just runs `sbx run --name`.

## Uninstall

```sh
paddock ls                      # remove sandboxes you no longer need with sbx rm
rm "$(command -v paddock)"
rm -rf ~/.config/paddock        # your config
rm -rf ~/Library/Caches/paddock ~/.cache/paddock   # kit and sandbox records
```

## Status

Early. Sandboxes are tested on macOS with `sbx` v0.49. On Linux (Ubuntu 24.04,
amd64 and arm64) the install script, the test suite and the commands that
don't need a sandbox are tested; running sandboxes there isn't yet, so reports
are welcome. Claude Code is the only agent for now.

paddock is an independent project, not affiliated with Docker or Anthropic.

## License

[MIT](LICENSE)
