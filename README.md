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
| `paddock --dry-run [-v] [DIR]` | Show what would be shared and which hooks would run, without starting anything. `-v` lists blocked hooks and the `sbx` command. |
| `paddock init` | Run the first-run setup, or re-check it. Never overwrites an existing config. |
| `paddock doctor` | Check `sbx`, config, git identity, SSH agent, Claude login, GitHub token, skills, and out-of-date sandboxes. |
| `paddock sync` | Copy `~/.claude/skills` into sandboxes after you change them. |
| `paddock ls` | List your paddock sandboxes by project folder, with status and when each was last used. |
| `paddock stop`, `shell` `[DIR]` | Stop a project's sandbox (conversations are kept), or open a shell in it. |
| `paddock rm [-f] [DIR]` | Remove a project's sandbox and its conversations. Asks first unless you pass `-f`. |

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
```

Unknown keys are an error, so a typo can't silently change what runs.

Settings apply when a sandbox is created. After you change them, `paddock` and
`paddock doctor` point out sandboxes created with older settings; recreate one
with `paddock rm` then `paddock`.

## What's shared, and what isn't

| From your machine | In the sandbox |
| --- | --- |
| The project folder | Read-write, at the same path |
| `~/.claude` entries in `link:` and `settings.json` | Read-only |
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

Early, and tested on macOS with `sbx` v0.49. Linux builds are published but
not yet tested; reports are welcome. Claude Code is the only agent for now.

paddock is an independent project, not affiliated with Docker or Anthropic.

## License

[MIT](LICENSE)
