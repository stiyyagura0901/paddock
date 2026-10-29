// Package config loads paddock's user config file.
package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"go.yaml.in/yaml/v3"
)

// Config is the user's paddock config. The zero value is not useful; use
// Default or Load.
type Config struct {
	// Extends is the path or https URL of a base config whose entries come
	// before these.
	Extends string `yaml:"extends"`
	// Link lists entries of ~/.claude mounted read-only and linked into the
	// sandbox's ~/.claude. settings.json is always mounted.
	Link    []string `yaml:"link"`
	Hooks   Hooks    `yaml:"hooks"`
	Plugins Plugins  `yaml:"plugins"`
	// Mounts are extra host folders: "PATH" (read-write) or "PATH:ro".
	Mounts []string `yaml:"mounts"`
	// Env entries are "KEY=VALUE", or "KEY" to pass the host's value.
	Env []string `yaml:"env"`
	// MCP lists sbx MCP server names (registered with `sbx mcp add`).
	MCP []string `yaml:"mcp"`
	// Projects holds per-project additions, keyed by folder path.
	Projects map[string]Project `yaml:"projects"`
}

// Hooks is the hooks block of a config.
type Hooks struct {
	// Allow keeps host hooks whose command starts with one of these.
	Allow []string `yaml:"allow"`
}

// Plugins is the plugins block of a config.
type Plugins struct {
	// Disable turns these host plugins off inside the sandbox.
	Disable []string `yaml:"disable"`
}

// Project is what a config adds for one project folder.
type Project struct {
	Link    []string `yaml:"link"`
	Hooks   Hooks    `yaml:"hooks"`
	Plugins Plugins  `yaml:"plugins"`
	Mounts  []string `yaml:"mounts"`
	Env     []string `yaml:"env"`
	MCP     []string `yaml:"mcp"`
}

// Starter returns the config paddock init writes: the default links plus
// extraLinks, and the given hook prefixes. With neither it parses to
// Default().
func Starter(extraLinks, allow []string) string {
	var links strings.Builder
	for _, l := range append(Default().Link, extraLinks...) {
		fmt.Fprintf(&links, "  - %s\n", strconv.Quote(l))
	}
	hooks := "  allow: []\n  #  - \"echo \"\n  #  - \"~/.claude/hooks/\"\n"
	if len(allow) > 0 {
		var b strings.Builder
		b.WriteString("  allow:\n")
		for _, a := range allow {
			fmt.Fprintf(&b, "    - %s\n", strconv.Quote(a))
		}
		hooks = b.String()
	}
	return `# paddock config. Changes apply to new sandboxes; remove a sandbox
# (paddock rm) to recreate it with new settings.

# A shared base config (a path or an https URL). Its entries come first.
# extends: ~/team/paddock.yaml

# Entries of ~/.claude mounted read-only and linked into the sandbox.
# settings.json is always mounted; only its portable parts are used.
link:
` + links.String() + `
hooks:
  # Host hooks run in the sandbox only if their command starts with one
  # of these prefixes. Project hooks never run.
` + hooks + `
plugins:
  # Host plugins to turn off inside the sandbox, as name@marketplace.
  disable: []

# Extra host folders for every sandbox. Add :ro to make one read-only.
# mounts:
#   - "~/code/shared-lib:ro"

# Environment variables: KEY=VALUE, or just KEY to pass this machine's value.
# env:
#   - "NODE_ENV=development"
#   - "NPM_TOKEN"

# MCP servers registered with sbx mcp add.
# mcp:
#   - notion

# Additions for one project folder, on top of the settings above.
# projects:
#   ~/code/my-project:
#     mounts: ["~/data/fixtures:ro"]
#     env: ["DATABASE_URL"]
`
}

// Default returns the config used when no file exists.
func Default() Config {
	var c Config
	c.Link = []string{"CLAUDE.md", "agents", "commands"}
	return c
}

// Path returns the config file location: $PADDOCK_CONFIG, else
// $XDG_CONFIG_HOME/paddock/config.yaml, else ~/.config/paddock/config.yaml.
func Path() (string, error) {
	if p := os.Getenv("PADDOCK_CONFIG"); p != "" {
		return p, nil
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "paddock", "config.yaml"), nil
}

// ExpandHome replaces a leading ~/ in p with home.
func ExpandHome(p, home string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

// For returns the config for project folder dir (absolute and free of
// symlinks): the global settings plus those of the matching project.
func (c Config) For(dir, home string) Config {
	out := c
	out.Extends, out.Projects = "", nil
	keys := make([]string, 0, len(c.Projects))
	for k := range c.Projects {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if resolve(k, home) != dir {
			continue
		}
		p := c.Projects[k]
		out.Link = merge(out.Link, p.Link)
		out.Hooks.Allow = merge(out.Hooks.Allow, p.Hooks.Allow)
		out.Plugins.Disable = merge(out.Plugins.Disable, p.Plugins.Disable)
		out.Mounts = merge(out.Mounts, p.Mounts)
		out.Env = merge(out.Env, p.Env)
		out.MCP = merge(out.MCP, p.MCP)
	}
	return out
}

func resolve(p, home string) string {
	p = filepath.Clean(ExpandHome(p, home))
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// merge returns a followed by b, without duplicates. It always copies.
func merge(a, b []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range append(append([]string{}, a...), b...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// Load reads the config at path, or returns Default if it does not exist.
// Unknown keys are an error so typos don't silently change behaviour. If
// the file extends a base config, the base's entries come first.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	c, err := parse(b, path)
	if err != nil || c.Extends == "" {
		return c, err
	}
	base, err := loadBase(c.Extends, path)
	if err != nil {
		return Config{}, err
	}
	return extend(base, c), nil
}

// CacheDir is where copies of https base configs are kept. Tests change it.
var CacheDir = func() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "paddock", "extends"), nil
}

// HTTPClient fetches https base configs. Tests change it.
var HTTPClient = http.DefaultClient

// cacheFor is how long a fetched base config is used before refetching.
const cacheFor = time.Hour

// loadBase reads the base config named by ref, from the file at from.
func loadBase(ref, from string) (Config, error) {
	var b []byte
	var err error
	switch {
	case strings.HasPrefix(ref, "http://"):
		return Config{}, fmt.Errorf("%s: extends %s: use https", from, ref)
	case strings.HasPrefix(ref, "https://"):
		b, err = fetchBase(ref)
	default:
		home, _ := os.UserHomeDir()
		ref = ExpandHome(ref, home)
		if !filepath.IsAbs(ref) {
			ref = filepath.Join(filepath.Dir(from), ref)
		}
		b, err = os.ReadFile(ref)
		if errors.Is(err, os.ErrNotExist) {
			err = fmt.Errorf("%s: extends %s: no such file", from, ref)
		}
	}
	if err != nil {
		return Config{}, err
	}
	base, err := decode(Config{}, b, ref)
	if err != nil {
		return Config{}, err
	}
	if base.Extends != "" {
		return Config{}, fmt.Errorf("%s: extends can only be one level deep", ref)
	}
	return base, nil
}

// fetchBase returns the body at url, from a cache younger than cacheFor
// if there is one. If fetching fails it falls back to an older copy.
func fetchBase(url string) ([]byte, error) {
	dir, err := CacheDir()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(url))
	cache := filepath.Join(dir, hex.EncodeToString(sum[:])[:16]+".yaml")
	fi, statErr := os.Stat(cache)
	if statErr == nil && time.Since(fi.ModTime()) < cacheFor {
		return os.ReadFile(cache)
	}
	b, err := fetch(url)
	if err != nil {
		if statErr != nil {
			return nil, fmt.Errorf("couldn't fetch %s: %w", url, err)
		}
		fmt.Fprintf(os.Stderr, "paddock: couldn't refresh %s (%v); using the copy from %s ago\n", url, err, age(time.Since(fi.ModTime())))
		return os.ReadFile(cache)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(cache, b, 0o644); err != nil {
		return nil, err
	}
	return b, nil
}

func fetch(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server said %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func age(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
}

// extend puts base's entries before c's. c.Extends is kept for display.
func extend(base, c Config) Config {
	out := c
	out.Link = merge(base.Link, c.Link)
	out.Hooks.Allow = merge(base.Hooks.Allow, c.Hooks.Allow)
	out.Plugins.Disable = merge(base.Plugins.Disable, c.Plugins.Disable)
	out.Mounts = merge(base.Mounts, c.Mounts)
	out.Env = merge(base.Env, c.Env)
	out.MCP = merge(base.MCP, c.MCP)
	if len(base.Projects)+len(c.Projects) > 0 {
		out.Projects = map[string]Project{}
	}
	for k, p := range base.Projects {
		out.Projects[k] = p
	}
	for k, p := range c.Projects {
		b := out.Projects[k]
		out.Projects[k] = Project{
			Link:    merge(b.Link, p.Link),
			Hooks:   Hooks{Allow: merge(b.Hooks.Allow, p.Hooks.Allow)},
			Plugins: Plugins{Disable: merge(b.Plugins.Disable, p.Plugins.Disable)},
			Mounts:  merge(b.Mounts, p.Mounts),
			Env:     merge(b.Env, p.Env),
			MCP:     merge(b.MCP, p.MCP),
		}
	}
	return out
}

func parse(b []byte, path string) (Config, error) {
	return decode(Default(), b, path)
}

var envRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(=.*)?$`)

// decode reads b over c and checks every entry. path names the source in
// errors.
func decode(c Config, b []byte, path string) (Config, error) {
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	home, _ := os.UserHomeDir()
	if err := check(c.Link, c.Mounts, c.Env, c.MCP, home); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	for k, p := range c.Projects {
		if !strings.HasPrefix(k, "/") && !strings.HasPrefix(k, "~/") {
			return Config{}, fmt.Errorf("%s: project %q must be a full path starting with / or ~/", path, k)
		}
		if err := check(p.Link, p.Mounts, p.Env, p.MCP, home); err != nil {
			return Config{}, fmt.Errorf("%s: project %s: %w", path, k, err)
		}
	}
	return c, nil
}

func check(links, mounts, env, mcp []string, home string) error {
	for _, l := range links {
		if l == "" || l == "." || l == ".." || filepath.Base(l) != l {
			return fmt.Errorf("link entry %q must be a plain name inside ~/.claude", l)
		}
	}
	for _, m := range mounts {
		p := strings.TrimSuffix(m, ":ro")
		if !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "~/") {
			return fmt.Errorf("mount %q must be a full path starting with / or ~/", m)
		}
		switch filepath.Clean(ExpandHome(p, home)) {
		case "/":
			return fmt.Errorf("mount %q would share your whole disk", m)
		case filepath.Clean(home):
			return fmt.Errorf("mount %q would share your whole home folder", m)
		}
	}
	for _, e := range env {
		if !envRe.MatchString(e) {
			return fmt.Errorf("env entry %q must be KEY=VALUE or KEY", e)
		}
	}
	for _, m := range mcp {
		if m == "" || strings.IndexFunc(m, unicode.IsSpace) >= 0 {
			return fmt.Errorf("mcp entry %q must be a server name without spaces", m)
		}
	}
	return nil
}

// WriteStarter writes the starter config to path unless a file is already
// there. It reports whether it wrote one.
func WriteStarter(path string, extraLinks, allow []string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := f.WriteString(Starter(extraLinks, allow)); err != nil {
		f.Close()
		return false, err
	}
	return true, f.Close()
}
