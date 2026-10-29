// Package kit holds the sbx kit that paddock applies to new sandboxes.
package kit

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed all:kit
var files embed.FS

// envPath is where the generated settings file lands inside the kit.
const envPath = "files/home/.local/share/paddock/env"

// Options are baked into the kit for the sandbox's startup scripts.
type Options struct {
	HostDir        string   // host ~/.claude, mounted read-only at the same path
	Links          []string // entries of HostDir linked into the sandbox's ~/.claude
	HookPrefixes   []string // host hooks whose command starts with one of these are kept
	DisablePlugins []string // plugins forced off inside the sandbox
	GitName        string   // host git identity, set in the sandbox if it has none
	GitEmail       string
}

// Write materialises the kit with opts into dir, replacing what was there.
func Write(dir string, opts Options) error {
	tmp, err := os.MkdirTemp(filepath.Dir(dir), ".kit-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	err = fs.WalkDir(files, "kit", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dst := filepath.Join(tmp, strings.TrimPrefix(p, "kit"))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := files.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
	if err != nil {
		return err
	}
	env, err := envFile(opts)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmp, envPath), []byte(env), 0o644); err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return os.Rename(tmp, dir)
}

// Fingerprint identifies the kit contents, options and mounts a sandbox is
// created with, so paddock can tell when an existing sandbox is out of date.
func Fingerprint(o Options, mounts []string) string {
	h := sha256.New()
	fs.WalkDir(files, "kit", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := files.ReadFile(p)
			fmt.Fprintf(h, "%s\x00%s\x00", p, b)
		}
		return nil
	})
	json.NewEncoder(h).Encode(struct {
		Options
		Mounts []string
	}{o, mounts})
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func envFile(o Options) (string, error) {
	prefixes, err := json.Marshal(nonNil(o.HookPrefixes))
	if err != nil {
		return "", err
	}
	plugins, err := json.Marshal(nonNil(o.DisablePlugins))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "PADDOCK_HOST_DIR=%s\n", quote(o.HostDir))
	fmt.Fprintf(&b, "PADDOCK_LINKS=%s\n", quote(strings.Join(o.Links, " ")))
	fmt.Fprintf(&b, "PADDOCK_HOOK_PREFIXES=%s\n", quote(string(prefixes)))
	fmt.Fprintf(&b, "PADDOCK_DISABLE_PLUGINS=%s\n", quote(string(plugins)))
	fmt.Fprintf(&b, "PADDOCK_GIT_NAME=%s\n", quote(o.GitName))
	fmt.Fprintf(&b, "PADDOCK_GIT_EMAIL=%s\n", quote(o.GitEmail))
	return b.String(), nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// quote single-quotes s for POSIX sh.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
