# Security policy

paddock's job is to keep a sandbox from changing your host Claude Code config
and to stop untrusted project hooks from running. A way around either of those
is a security bug.

## Reporting a vulnerability

Please report privately through
[GitHub's vulnerability reporting](https://github.com/stiyyagura0901/paddock/security/advisories/new),
not in a public issue. Include the paddock and `sbx` versions
(`paddock version`, `sbx version`), your OS, and steps to reproduce.

paddock is maintained by one person, so replies are best effort. Once a fix is
released, the advisory is published with credit to you unless you'd rather
stay anonymous.

## In scope

- A project's `.claude/settings.json` hooks, or a plugin's hooks, running in a
  paddock sandbox.
- A sandbox writing to anything paddock mounts read-only, such as `~/.claude`.
- Host hooks running that don't match `hooks.allow`.
- paddock exposing host files or secrets it doesn't document sharing.

## Out of scope

- Weaknesses in Docker Sandboxes (`sbx`) itself or in Claude Code; report those
  to Docker or Anthropic.
- Anything Claude does to the project folder, which is read-write by design.
- Values you put in the `env` block of your own `settings.json`, which paddock
  copies into the sandbox as documented.

## Supported versions

Fixes go into the latest release only.
