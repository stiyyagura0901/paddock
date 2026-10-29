# Prepares a fake, generic home for docs/demo.tape. Sourced by the tape's
# hidden setup inside the VHS container; not meant to run on your machine.

export HOME=/home/dev
export PATH="/vhs/.demo-bin:$PATH"
unset XDG_CONFIG_HOME PADDOCK_CONFIG

mkdir -p "$HOME/.claude/agents" "$HOME/.claude/commands" "$HOME/.claude/hooks" \
	"$HOME/.config/paddock" "$HOME/code/my-app"

echo "Prefer small, focused changes." >"$HOME/.claude/CLAUDE.md"
echo "Review the diff for bugs." >"$HOME/.claude/agents/reviewer.md"
echo "Run the tests, then commit." >"$HOME/.claude/commands/ship.md"
printf '#!/bin/sh\ngofmt -w .\n' >"$HOME/.claude/hooks/format.sh"
chmod +x "$HOME/.claude/hooks/format.sh"

cat >"$HOME/.claude/settings.json" <<'EOF'
{
  "model": "opus",
  "enabledPlugins": {"code-review@claude-plugins-official": true, "gopls-lsp@claude-plugins-official": true, "commit-commands@claude-plugins-official": true},
  "hooks": {
    "UserPromptSubmit": [{"hooks": [{"type": "command", "command": "echo 'Keep replies short.'"}]}],
    "PostToolUse": [{"matcher": "Edit", "hooks": [{"type": "command", "command": "~/.claude/hooks/format.sh"}]}],
    "Notification": [{"hooks": [{"type": "command", "command": "osascript -e 'display notification \"Claude needs you\"'"}]}],
    "Stop": [{"hooks": [{"type": "command", "command": "afplay /System/Library/Sounds/Glass.aiff"}]}]
  }
}
EOF

cat >"$HOME/.config/paddock/config.yaml" <<'EOF'
link: [CLAUDE.md, agents, commands, hooks]
hooks:
  allow: ["echo ", "~/.claude/hooks/format.sh"]
plugins:
  disable: []
EOF

cd "$HOME"
export PS1='$ '
