package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Adding a server must only add lines, so comments and blank lines in a
// hand-edited file survive, and must land where Load reads it.
func TestAddMCP(t *testing.T) {
	const home = "/home/me"
	for _, tc := range []struct {
		name, project, in, want string
	}{
		{"starter file", "", Starter(nil, nil, nil), strings.Replace(Starter(nil, nil, nil), "# mcp:\n#   - linear\n", "mcp:\n  - linear\n", 1)},
		{"block list", "", "link: [a]\n\nmcp:\n  - notion # docs\n\n# end\n", "link: [a]\n\nmcp:\n  - notion # docs\n  - linear\n\n# end\n"},
		{"flow list", "", "mcp: [notion]\n", "mcp: [notion, linear]\n"},
		{"empty list", "", "mcp: []\nenv: [A]\n", "mcp: [linear]\nenv: [A]\n"},
		{"null", "", "mcp:\nenv: [A]\n", "mcp:\n  - linear\nenv: [A]\n"},
		{"already there", "", "mcp: [linear]\n", "mcp: [linear]\n"},
		{"empty file", "", "", "mcp:\n  - linear\n"},
		{"new projects", "~/app", "link: [a]\n", "link: [a]\n\nprojects:\n  ~/app:\n    mcp:\n      - linear\n"},
		{"new project", "~/app", "projects:\n  /other:\n    env: [A]\n# tail\n", "projects:\n  /other:\n    env: [A]\n  ~/app:\n    mcp:\n      - linear\n# tail\n"},
		{"project by full path", "~/app", "projects:\n  /home/me/app:\n    env: [A]\n", "projects:\n  /home/me/app:\n    env: [A]\n    mcp:\n      - linear\n"},
		{"project list", "~/app", "projects:\n  ~/app:\n    mcp:\n    - notion\n", "projects:\n  ~/app:\n    mcp:\n    - notion\n    - linear\n"},
	} {
		got, err := addMCP([]byte(tc.in), tc.project, home, []string{"linear"})
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if string(got) != tc.want {
			t.Errorf("%s:\ngot:\n%s\nwant:\n%s", tc.name, got, tc.want)
		}
	}
}

func TestAddMCPRefusesLayoutsItCantEdit(t *testing.T) {
	for _, tc := range []struct{ project, in string }{
		{"", "mcp: [notion,\n  fetch]\n"},
		{"~/app", "projects: {\"~/app\": {env: [A]}}\n"},
		// A commented-out example inside another block isn't a safe place.
		{"", "projects:\n  ~/a:\n    env: [A]\n# mcp:\n#   - notion\n  ~/b:\n    env: [B]\n"},
	} {
		if _, err := addMCP([]byte(tc.in), tc.project, "/home/me", []string{"linear"}); err == nil {
			t.Errorf("%q: expected an error", tc.in)
		}
	}
}

// Removing a server takes it off the global list and this project's, but
// not other projects'.
func TestRemoveMCP(t *testing.T) {
	in := "mcp:\n  - linear\n  - notion\nprojects:\n  ~/app:\n    mcp: [linear]\n  ~/other:\n    mcp: [linear]\n"
	got, removed, err := removeMCP([]byte(in), "/home/me/app", "/home/me", []string{"linear", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	want := "mcp:\n  - notion\nprojects:\n  ~/app:\n    mcp: []\n  ~/other:\n    mcp: [linear]\n"
	if string(got) != want || !reflect.DeepEqual(removed, []string{"linear"}) {
		t.Errorf("got %v and:\n%s\nwant:\n%s", removed, got, want)
	}
	got, _, err = removeMCP([]byte("mcp:\n  - linear\nenv: [A]\n"), "/x", "/home/me", []string{"linear"})
	if err != nil || string(got) != "mcp: []\nenv: [A]\n" {
		t.Errorf("emptied list: got %q, %v", got, err)
	}
}

func TestAddMCPWritesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte(Starter(nil, nil, nil)), 0o600)
	if err := AddMCP(path, "", []string{"linear", "circleci"}); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil || !reflect.DeepEqual(c.MCP, []string{"linear", "circleci"}) {
		t.Errorf("got %v, %v", c.MCP, err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode changed to %v", fi.Mode().Perm())
	}
}
