// Package sandbox maps a project directory to its sandbox.
package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
		return "", fmt.Errorf("there's no folder at %s", dir)
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("%s is a file; give paddock a project folder", dir)
	}
	if h, err := filepath.EvalSymlinks(home); err == nil {
		home = h
	}
	switch abs {
	case "/":
		return "", errors.New("won't share your whole disk with a sandbox. Run paddock inside a project folder")
	case filepath.Clean(home):
		return "", errors.New("won't share your whole home folder with a sandbox. Run paddock inside a project folder, for example: cd ~/code/my-project && paddock")
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

// NamedName returns the name of an extra sandbox called name for a
// project directory, next to the one Name gives it.
func NamedName(dir, name string) string {
	return Name(dir) + "-" + name
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)

// CheckName lowercases a sandbox name given with --name and checks it.
func CheckName(name string) (string, error) {
	n := strings.ToLower(name)
	if !nameRe.MatchString(n) {
		return "", fmt.Errorf("%q can't be a sandbox name: use up to 31 lowercase letters, digits and dashes, starting with a letter or digit. For example: paddock --name fix-login", name)
	}
	return n, nil
}

// Owned reports whether a sandbox called sbxName with first workspace ws
// is one paddock made: the project's main sandbox (named is "") or one
// of its named ones.
func Owned(sbxName, ws string) (named string, ok bool) {
	main := Name(ws)
	if sbxName == main {
		return "", true
	}
	if rest, found := strings.CutPrefix(sbxName, main+"-"); found && nameRe.MatchString(rest) {
		return rest, true
	}
	return "", false
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
