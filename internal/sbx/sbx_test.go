package sbx

import (
	"os"
	"path/filepath"
	"testing"
)

// sbx can print a notice after its JSON, which must not break parsing.
func TestJSONWithTrailingNotice(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "sbx")
	os.WriteFile(bin, []byte(`#!/bin/sh
case "$1" in
ls) echo '[{"name": "claude-app-123abc", "workspaces": ["/work/app"]}]' ;;
mcp) echo '[{"name": "linear"}]' ;;
esac
echo
echo '✨ Docker Sandboxes update available: v9.9.9 (running v1.0.0)'
`), 0o755)
	defer func(old string) { Bin = old }(Bin)
	Bin = bin

	list, err := List()
	if err != nil || len(list) != 1 || list[0].Name != "claude-app-123abc" {
		t.Errorf("List() = %+v, %v", list, err)
	}
	servers, err := MCPServers()
	if err != nil || !servers["linear"] {
		t.Errorf("MCPServers() = %v, %v", servers, err)
	}
}
