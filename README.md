# paddock

**Run Claude Code in Docker Sandboxes with your own `~/.claude` config — read-only, with project hooks blocked.**

Docker Sandboxes (`sbx`) gives Claude Code an isolated microVM where it can run
with `--dangerously-skip-permissions` safely. But a fresh sandbox starts with
none of your setup: no `CLAUDE.md`, no custom agents or slash commands, no
plugins, no git identity, and `gh` isn't logged in. paddock is a small CLI on
top of `sbx` that fixes that in one command, without letting the sandbox touch
your host config.

```sh
cd ~/code/my-project
paddock
```

## Why you might want this

You've installed Docker Sandboxes and hit one of these:

- **Claude Code in the sandbox ignores my `~/.claude/CLAUDE.md`, agents and commands.**
  `sbx` doesn't import user-level Claude config. paddock mounts it read-only.
- **I want my settings in the sandbox, but I don't want the sandbox to be able to change them.**
  Host config is mounted read-only; the sandbox gets its own `settings.json`
  built from the portable parts of yours (model, env, plugins, marketplaces,
  allowed permissions).
- **`gh` fails inside the sandbox with `Failed to log in to github.com using token (GH_TOKEN)`.**
  `paddock doctor` spots the missing token and prints the fix.
- **`git commit` in the sandbox fails with `Author identity unknown`.**
  paddock copies your host git name and email into new sandboxes.
- **I don't want to run a cloned repo's `.claude/settings.json` hooks.**
  A repo's hooks are arbitrary shell commands. paddock makes the hooks you
  choose the only hooks that can run; project and plugin hooks are blocked.
- **My host hooks call scripts that don't exist in the sandbox.**
  Only hooks you allow are installed, so notification and audit hooks that
  need your Mac stay on your Mac.
- **I have two projects called `api` and they ended up in the same sandbox.**
  paddock names each sandbox after the folder plus a short hash of its full path.
- **I exited the sandbox and want my conversation back.**
  `paddock -- --continue`.

## Install

Requires [Docker Sandboxes](https://docs.docker.com/ai/sandboxes/) (`sbx`).

**Release binary** (macOS and Linux, amd64 and arm64):

```sh
curl -fsSL https://raw.githubusercontent.com/stiyyagura0901/paddock/main/install.sh | sh
```

This installs `paddock` to `~/.local/bin`. Set `PADDOCK_INSTALL_DIR` to change
that. You can also download an archive from
[Releases](https://github.com/stiyyagura0901/paddock/releases) yourself.

**With Go** (1.24+):

```sh
go install github.com/stiyyagura0901/paddock/cmd/paddock@latest
```

**From source:**

```sh
git clone https://github.com/stiyyagura0901/paddock && cd paddock
go build -o paddock ./cmd/paddock
```

## Quick start

```sh
paddock init      # writes a config, offers your portable hooks, checks your setup
cd ~/code/my-project
paddock --dry-run # optional: see exactly what will be shared and run
paddock           # start Claude Code in this project's sandbox
```

`paddock init` ends by running `paddock doctor`, which tells you the exact
command for anything missing, such as saving your GitHub token:

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

## Commands

| Command | What it does |
| --- | --- |
| `paddock [DIR] [-- ARGS]` | Start or reattach to DIR's sandbox (default `.`). Arguments after `--` go to Claude, e.g. `paddock -- --continue` or `paddock -- --resume`. |
| `paddock --dry-run [-v] [DIR]` | Show what would be shared and which hooks would run, without starting anything. `-v` lists blocked hooks and the `sbx` command. |
| `paddock init` | First-run setup: config, kit, and `doctor`. Never overwrites an existing config. |
| `paddock doctor` | Check `sbx`, config, git identity, SSH agent, Claude login, GitHub token, skills, and out-of-date sandboxes. |
| `paddock sync` | Re-import `~/.claude/skills` into the sandbox skill store after you change them. |
| `paddock ls` / `stop` / `rm` / `shell` | List, stop, remove, or open a shell in a project's sandbox. `rm` deletes its conversations. |

## What gets shared

| From your host | In the sandbox |
| --- | --- |
| The project folder | Read-write, at the same path |
| `CLAUDE.md`, `agents/`, `commands/` (configurable) | Read-only, linked into the sandbox's `~/.claude` |
| `settings.json` | Read-only; model, effort level, env, plugins, marketplaces and `permissions.allow` are copied into the sandbox's own settings |
| Hooks you allow | Installed as managed hooks; everything else is blocked |
| Skills | Via `sbx skills import` (`paddock sync`) |
| git name and email, SSH agent | Copied / forwarded |

Your host `~/.claude` is never written to.

## Configuration

`~/.config/paddock/config.yaml` (or `$XDG_CONFIG_HOME/paddock/config.yaml`, or
`$PADDOCK_CONFIG`):

```yaml
# Entries of ~/.claude mounted read-only and linked into the sandbox.
link:
  - CLAUDE.md
  - agents
  - commands

hooks:
  # Host hooks run in the sandbox only if their command starts with one of
  # these prefixes. Project and plugin hooks never run.
  allow:
    - "echo "
    - "~/.claude/hooks/"   # add "hooks" to link: too, so the scripts exist

plugins:
  # Host plugins to turn off inside the sandbox, as name@marketplace.
  disable: []
```

Unknown keys are an error, so a typo can't silently change what runs.

Config is applied when a sandbox is created. After changing it, `paddock`
warns about sandboxes created with older settings; remove one with
`paddock rm` and run `paddock` again to recreate it.

## Troubleshooting

**`gh auth status` fails in the sandbox.** Save a token for all sandboxes:
`sbx secret set github --command 'gh auth token'`. New and restarted
sandboxes pick it up.

**A plugin from a private GitHub marketplace won't install.** Same fix: the
sandbox needs the GitHub token to clone the marketplace.

**My hook doesn't run in the sandbox.** Run `paddock --dry-run -v` to see
whether it's allowed. If it calls a script, the script's folder must be in
`link:` and the hook must refer to it by a path under `~/.claude`.

**I changed my config and nothing happened.** Existing sandboxes keep the
settings they were created with. `paddock doctor` lists out-of-date ones.

**My skills are missing.** Run `paddock sync`.

**`git push` fails in the sandbox.** Check `ssh-add -l` on the host lists a
key; `sbx` forwards your SSH agent.

## How it works

paddock is a thin wrapper around `sbx run`. For a new sandbox it:

1. Mounts the project read-write and your chosen `~/.claude` entries read-only.
2. Applies a small [kit](https://docs.docker.com/ai/sandboxes/) embedded in the
   binary. At startup the kit links your config into the sandbox's
   `~/.claude`, builds the sandbox's `settings.json` from the portable parts of
   yours, and writes `/etc/claude-code/managed-settings.json` with your allowed
   hooks and `allowManagedHooksOnly: true`. Managed settings outrank project
   and plugin settings, which is what blocks every other hook.
3. Remembers a fingerprint of those settings, so it can tell you later when a
   sandbox is out of date.

Reattaching just runs `sbx run --name <sandbox>`.

## Status

Early. Tested on macOS with `sbx` v0.49. Linux builds are published but
untested so far; reports welcome. Claude Code is the only agent for now.

paddock is an independent project and is not affiliated with Docker or Anthropic.

## License

[MIT](LICENSE)
