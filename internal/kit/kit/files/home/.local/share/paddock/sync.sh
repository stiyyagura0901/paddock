#!/bin/sh
# Wire read-only host Claude config into this sandbox's ~/.claude. Never writes to the host.
set -u
. "$HOME/.local/share/paddock/env"
H="$PADDOCK_HOST_DIR"
C="$HOME/.claude"
mkdir -p "$C"

# Host git identity, unless the sandbox already has its own.
if [ -n "$PADDOCK_GIT_NAME" ] && [ -z "$(git config --global user.name 2>/dev/null)" ]; then
  git config --global user.name "$PADDOCK_GIT_NAME"
fi
if [ -n "$PADDOCK_GIT_EMAIL" ] && [ -z "$(git config --global user.email 2>/dev/null)" ]; then
  git config --global user.email "$PADDOCK_GIT_EMAIL"
fi

# Live links to host files; skip anything the sandbox already owns as a real directory.
for p in $PADDOCK_LINKS; do
  [ -e "$H/$p" ] || continue
  [ -d "$C/$p" ] && [ ! -L "$C/$p" ] && continue
  ln -sfn "$H/$p" "$C/$p"
done

if [ ! -f "$H/settings.json" ]; then
  echo '{}' > "$C/.host-hooks.json"
  exit 0
fi

# Allowed host hooks only; managed.sh installs them as managed hooks.
jq --argjson prefixes "$PADDOCK_HOOK_PREFIXES" '(.hooks // {})
  | map_values(map(.hooks |= map(select((.command // "") as $c | any($prefixes[]; . as $p | $c | startswith($p)))))
    | map(select(.hooks | length > 0)))
  | with_entries(select(.value | length > 0))' "$H/settings.json" > "$C/.host-hooks.json"

# Sandbox-only settings: the sandbox defaults plus the portable subset of host settings.
base="$C/settings.sandbox-base.json"
[ -f "$base" ] || cp "$C/settings.json" "$base" 2>/dev/null || echo '{}' > "$base"
jq -s --argjson off "$PADDOCK_DISABLE_PLUGINS" '.[0] * (.[1] | {
    model, effortLevel, env, enabledPlugins, extraKnownMarketplaces,
    permissions: {allow: (.permissions.allow // [])}
  } | with_entries(select(.value != null)))
  | reduce $off[] as $p (.; .enabledPlugins[$p] = false)' "$base" "$H/settings.json" > "$C/settings.json.tmp" \
  && mv "$C/settings.json.tmp" "$C/settings.json"
