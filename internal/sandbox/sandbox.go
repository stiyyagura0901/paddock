// Package sandbox maps a project directory to its sandbox.
package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Agent is the only agent supported in v1.
const Agent = "claude"

// ResolveDir returns the absolute, symlink-free project directory and
// refuses directories too broad to mount.
func ResolveDir(dir, home string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("no such directory: %s", dir)
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("not a directory: %s", dir)
	}
	if h, err := filepath.EvalSymlinks(home); err == nil {
		home = h
	}
	if abs == "/" || abs == filepath.Clean(home) {
		return "", errors.New("refusing to mount " + abs + "; pick a project directory")
	}
	return abs, nil
}

// Name returns the sandbox name for a project directory: the agent, the
// directory's base name, and a short hash of the full path so that two
// projects with the same base name get different sandboxes.
func Name(dir string) string {
	sum := sha256.Sum256([]byte(dir))
	return fmt.Sprintf("%s-%s-%s", Agent, sanitize(filepath.Base(dir)), hex.EncodeToString(sum[:])[:6])
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-.")
}
