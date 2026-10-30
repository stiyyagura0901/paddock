package hostcfg

import (
	"os"
	"path/filepath"
	"testing"
)

// A project's own server wins over a global one of the same name, and
// other projects' servers are left out.
func TestMCPServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	os.WriteFile(path, []byte(`{
		"mcpServers": {
			"linear": {"type": "http", "url": "https://mcp.linear.app/mcp"},
			"circleci": {"command": "circleci", "args": ["mcp", "start"], "env": {"CIRCLE_TOKEN": "secret"}}
		},
		"projects": {
			"/work/app": {"mcpServers": {"linear": {"type": "http", "url": "https://other"}}},
			"/work/other": {"mcpServers": {"db": {"command": "db"}}}
		}
	}`), 0o644)
	got, err := MCPServers(path, "/work/app")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "circleci" || got[1].Name != "linear" {
		t.Fatalf("got %+v", got)
	}
	if c := got[0]; c.Type != "stdio" || c.Project || len(c.Env) != 1 || c.Env[0] != "CIRCLE_TOKEN" {
		t.Errorf("circleci = %+v", c)
	}
	if l := got[1]; l.URL != "https://other" || !l.Project {
		t.Errorf("linear = %+v", l)
	}
	if none, err := MCPServers(filepath.Join(dir, "missing.json"), "/work/app"); err != nil || none != nil {
		t.Errorf("missing file: %v, %v", none, err)
	}
}
