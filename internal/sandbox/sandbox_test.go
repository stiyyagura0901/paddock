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
