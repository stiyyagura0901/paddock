package hostcfg

import "testing"

func TestSuggest(t *testing.T) {
	links := []string{"CLAUDE.md", "knowledge"}
	for _, tc := range []struct {
		cmd, want string
		ok        bool
	}{
		{`echo '{"a":1}'`, "echo ", true},
		{"~/.claude/knowledge/start.sh", "~/.claude/knowledge/start.sh", true},
		{"bash /home/u/.claude/knowledge/x.sh arg", "bash /home/u/.claude/knowledge/x.sh", true},
		{"bash /home/u/.claude/hooks/notify.sh", "", false}, // hooks/ is not linked
		{"/home/u/bin/usage.sh", "", false},
		{`if [ -z "$HOME" ]; then :; fi`, "", false},
	} {
		got, ok := Suggest(tc.cmd, links, "/home/u")
		if got != tc.want || ok != tc.ok {
			t.Errorf("Suggest(%q) = %q, %v; want %q, %v", tc.cmd, got, ok, tc.want, tc.ok)
		}
	}
}
