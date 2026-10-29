#!/bin/sh
# Runs as root: make the allowed host hooks the only hooks in this sandbox.
set -u
hooks=/home/agent/.claude/.host-hooks.json
[ -f "$hooks" ] || echo '{}' > "$hooks"
mkdir -p /etc/claude-code
jq '{allowManagedHooksOnly: true, hooks: .}' "$hooks" > /etc/claude-code/managed-settings.json
chmod 0644 /etc/claude-code/managed-settings.json
