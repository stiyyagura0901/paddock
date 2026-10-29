package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The starter file must parse, and hook prefixes with quotes must survive.
func TestStarterParses(t *testing.T) {
	allow := []string{"echo ", `~/.claude/x "q".sh`}
	c, err := parse([]byte(Starter([]string{"hooks"}, allow)), "starter")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Hooks.Allow, allow) || !reflect.DeepEqual(c.Link, append(Default().Link, "hooks")) {
		t.Errorf("got %+v", c)
	}
}

func TestLoadRejectsBadInput(t *testing.T) {
	for name, body := range map[string]string{
		"typo in key":  "hook:\n  allow: []\n",
		"path in link": "link: [../secrets]\n",
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
	if wrote, err := WriteStarter(path, nil, nil); err != nil || wrote {
		t.Fatalf("wrote=%v err=%v", wrote, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "link: []\n" {
		t.Error("existing config overwritten")
	}
}
