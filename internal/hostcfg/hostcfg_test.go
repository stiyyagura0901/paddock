package hostcfg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSuggest(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{"knowledge", "hooks"} {
		os.MkdirAll(filepath.Join(home, ".claude", d), 0o755)
	}
	links := []string{"CLAUDE.md", "knowledge"}
	for _, tc := range []struct {
		cmd, prefix, needLink string
		ok                    bool
	}{
		{`echo '{"a":1}'`, "echo ", "", true},
		{"~/.claude/knowledge/start.sh", "~/.claude/knowledge/start.sh", "", true},
		{"bash " + home + "/.claude/knowledge/x.sh arg", "bash " + home + "/.claude/knowledge/x.sh", "", true},
		// Works once hooks/ is linked too.
		{"bash " + home + "/.claude/hooks/notify.sh", "bash " + home + "/.claude/hooks/notify.sh", "hooks", true},
		{"~/.claude/missing/x.sh", "", "", false},
		{"/opt/bin/usage.sh", "", "", false},
		{`if [ -z "$HOME" ]; then :; fi`, "", "", false},
	} {
		prefix, needLink, ok := Suggest(tc.cmd, links, home)
		if prefix != tc.prefix || needLink != tc.needLink || ok != tc.ok {
			t.Errorf("Suggest(%q) = %q, %q, %v; want %q, %q, %v", tc.cmd, prefix, needLink, ok, tc.prefix, tc.needLink, tc.ok)
		}
	}
}
