package kit

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Runs the real sync.sh against a fake host and sandbox home, as the
// sandbox would at startup. Values with quotes must survive the env file.
func TestSyncScript(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not installed")
	}
	root := t.TempDir()
	host := filepath.Join(root, "it's host", ".claude")
	os.MkdirAll(filepath.Join(host, "agents"), 0o755)
	os.WriteFile(filepath.Join(host, "CLAUDE.md"), []byte("hi"), 0o644)
	os.WriteFile(filepath.Join(host, "settings.json"), []byte(`{
	  "model": "m1",
	  "permissions": {"allow": ["Bash(ls:*)"], "deny": ["x"]},
	  "statusLine": {"command": "/host/only.sh"},
	  "enabledPlugins": {"keep@m": true, "drop@m": true},
	  "hooks": {
	    "Stop": [{"hooks": [{"type": "command", "command": "/host/notify.sh"}]}],
	    "SessionStart": [{"hooks": [{"type": "command", "command": "echo hi"}, {"type": "prompt"}]}]
	  }
	}`), 0o644)

	kitDir := filepath.Join(root, "kit")
	err := Write(kitDir, Options{
		HostDir:        host,
		Links:          []string{"CLAUDE.md", "agents", "missing"},
		HookPrefixes:   []string{"echo "},
		DisablePlugins: []string{"drop@m"},
	})
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(kitDir, "files", "home")
	cmd := exec.Command("sh", filepath.Join(home, ".local/share/paddock/sync.sh"))
	cmd.Env = append(os.Environ(), "HOME="+home, "GIT_CONFIG_GLOBAL="+filepath.Join(root, "gitconfig"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sync.sh: %v\n%s", err, out)
	}

	c := filepath.Join(home, ".claude")
	if target, err := os.Readlink(filepath.Join(c, "agents")); err != nil || target != filepath.Join(host, "agents") {
		t.Errorf("agents link = %q, %v", target, err)
	}
	if _, err := os.Lstat(filepath.Join(c, "missing")); err == nil {
		t.Error("linked an entry that does not exist on the host")
	}

	var hooks map[string][]struct{ Hooks []struct{ Command string } }
	readJSON(t, filepath.Join(c, ".host-hooks.json"), &hooks)
	if len(hooks) != 1 || len(hooks["SessionStart"]) != 1 || hooks["SessionStart"][0].Hooks[0].Command != "echo hi" {
		t.Errorf("hooks = %+v, want only the echo hook", hooks)
	}

	var s map[string]any
	readJSON(t, filepath.Join(c, "settings.json"), &s)
	if s["model"] != "m1" || s["statusLine"] != nil {
		t.Errorf("settings should carry model but not host-only statusLine: %v", s)
	}
	plugins := s["enabledPlugins"].(map[string]any)
	if plugins["keep@m"] != true || plugins["drop@m"] != false {
		t.Errorf("enabledPlugins = %v", plugins)
	}
	if perms := s["permissions"].(map[string]any); perms["deny"] != nil {
		t.Errorf("only permissions.allow should carry over: %v", perms)
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}
