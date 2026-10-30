package config

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// The starter file must parse, and hook prefixes with quotes must survive.
func TestStarterParses(t *testing.T) {
	allow := []string{"echo ", `~/.claude/x "q".sh`}
	c, err := parse([]byte(Starter([]string{"hooks"}, allow, []string{"linear"})), "starter")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Hooks.Allow, allow) || !reflect.DeepEqual(c.Link, append(Default().Link, "hooks")) || !reflect.DeepEqual(c.MCP, []string{"linear"}) {
		t.Errorf("got %+v", c)
	}
}

func TestLoadRejectsBadInput(t *testing.T) {
	for name, body := range map[string]string{
		"typo in key":          "hook:\n  allow: []\n",
		"path in link":         "link: [../secrets]\n",
		"typo in project":      "projects:\n  /code/x:\n    mount: [/data]\n",
		"bad env name":         "env: [\"1KEY=v\"]\n",
		"mount of home":        "mounts: [\"~/:ro\"]\n",
		"relative mount":       "mounts: [data]\n",
		"relative project":     "projects:\n  code/x:\n    env: [A]\n",
		"path in project link": "projects:\n  /code/x:\n    link: [../x]\n",
	} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		os.WriteFile(path, []byte(body), 0o644)
		if _, err := Load(path); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestWriteStarterKeepsExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("link: []\n"), 0o644)
	if wrote, err := WriteStarter(path, nil, nil, nil); err != nil || wrote {
		t.Fatalf("wrote=%v err=%v", wrote, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "link: []\n" {
		t.Error("existing config overwritten")
	}
}

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestForAddsMatchingProject(t *testing.T) {
	home := t.TempDir()
	proj := filepath.Join(home, "code", "app")
	os.MkdirAll(proj, 0o755)
	proj, _ = filepath.EvalSymlinks(proj)
	c := Config{
		Env:    []string{"A=1"},
		Mounts: []string{"/data:ro"},
		Projects: map[string]Project{
			"~/code/app":  {Env: []string{"A=1", "B"}, MCP: []string{"notion"}},
			"/code/other": {Env: []string{"C"}},
		},
	}
	got := c.For(proj, home)
	if !reflect.DeepEqual(got.Env, []string{"A=1", "B"}) || !reflect.DeepEqual(got.MCP, []string{"notion"}) ||
		!reflect.DeepEqual(got.Mounts, []string{"/data:ro"}) || got.Projects != nil {
		t.Errorf("got %+v", got)
	}
	if other := c.For(filepath.Join(home, "code"), home); !reflect.DeepEqual(other.Env, []string{"A=1"}) {
		t.Errorf("non-matching project applied: %v", other.Env)
	}
}

func TestExtendsLocalFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "team.yaml", "env: [TEAM=1, SHARED]\nprojects:\n  /code/x:\n    mcp: [notion]\n")
	path := write(t, dir, "config.yaml", "extends: team.yaml\nenv: [SHARED, MINE, TEAM=2]\nprojects:\n  /code/x:\n    mcp: [notion, linear]\n")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Env, []string{"TEAM=2", "SHARED", "MINE"}) ||
		!reflect.DeepEqual(c.Projects["/code/x"].MCP, []string{"notion", "linear"}) || c.Extends != "team.yaml" {
		t.Errorf("got %+v", c)
	}

	write(t, dir, "team.yaml", "extends: other.yaml\n")
	if _, err := Load(path); err == nil {
		t.Error("base with its own extends accepted")
	}
}

func TestExtendsHTTPSFallsBackToCache(t *testing.T) {
	cache := t.TempDir()
	oldDir, oldClient := CacheDir, HTTPClient
	t.Cleanup(func() { CacheDir, HTTPClient = oldDir, oldClient })
	CacheDir = func() (string, error) { return cache, nil }

	up, body := true, "mcp: [notion]\n"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		w.Write([]byte(body))
	}))
	defer srv.Close()
	HTTPClient = srv.Client()
	path := write(t, t.TempDir(), "config.yaml", "extends: "+srv.URL+"/team.yaml\n")

	c, err := Load(path)
	if err != nil || !reflect.DeepEqual(c.MCP, []string{"notion"}) {
		t.Fatalf("fetch: %+v, %v", c, err)
	}
	files, _ := filepath.Glob(filepath.Join(cache, "*.yaml"))
	if len(files) != 1 {
		t.Fatalf("cache files: %v", files)
	}

	// An expired cache is refetched; when that fails the old copy is used.
	up = false
	old := time.Now().Add(-2 * time.Hour)
	os.Chtimes(files[0], old, old)
	if c, err := Load(path); err != nil || !reflect.DeepEqual(c.MCP, []string{"notion"}) {
		t.Errorf("fallback: %+v, %v", c, err)
	}

	// A remote base may not widen what sandboxes can see.
	up, body = true, "mounts: [\"~/.aws:ro\"]\n"
	os.Chtimes(files[0], old, old)
	if _, err := Load(path); err == nil {
		t.Error("remote base with mounts accepted")
	}
}
