package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

// Two projects with the same folder name must not share a sandbox.
func TestNameDistinguishesSameBaseName(t *testing.T) {
	if a, b := Name("/work/one/api"), Name("/work/two/api"); a == b {
		t.Fatalf("same name for different paths: %s", a)
	}
}

// A named sandbox must never take the project's main sandbox name, or
// another name's.
func TestNamedNameIsDistinct(t *testing.T) {
	main := Name("/work/api")
	a, b := NamedName("/work/api", "feat-a"), NamedName("/work/api", "feat-b")
	if a == main || b == main || a == b {
		t.Fatalf("names collide: %s, %s, %s", main, a, b)
	}
}

func TestCheckName(t *testing.T) {
	if n, err := CheckName("Feat-A"); err != nil || n != "feat-a" {
		t.Errorf("CheckName(Feat-A) = %q, %v; want feat-a", n, err)
	}
	for _, bad := range []string{"", "-x", "a_b", "a b", "a/b", "ünï", "abcdefghijklmnopqrstuvwxyz0123456"} {
		if _, err := CheckName(bad); err == nil {
			t.Errorf("CheckName(%q) accepted it", bad)
		}
	}
}

func TestResolveDirRefusesBroadMounts(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{"/", home} {
		if _, err := ResolveDir(d, home); err == nil {
			t.Errorf("expected refusal for %s", d)
		}
	}
	proj := filepath.Join(home, "proj")
	if err := os.Mkdir(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveDir(proj, home); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
