// Package config loads paddock's user config file.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Config is the user's paddock config. The zero value is not useful; use
// Default or Load.
type Config struct {
	// Link lists entries of ~/.claude mounted read-only and linked into the
	// sandbox's ~/.claude. settings.json is always mounted.
	Link  []string `yaml:"link"`
	Hooks struct {
		// Allow keeps host hooks whose command starts with one of these.
		Allow []string `yaml:"allow"`
	} `yaml:"hooks"`
	Plugins struct {
		// Disable turns these host plugins off inside the sandbox.
		Disable []string `yaml:"disable"`
	} `yaml:"plugins"`
}

// Starter returns the config paddock init writes, allowing the given hook
// prefixes. With no prefixes it parses to Default().
func Starter(allow []string) string {
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

# Entries of ~/.claude mounted read-only and linked into the sandbox.
# settings.json is always mounted; only its portable parts are used.
link:
  - CLAUDE.md
  - agents
  - commands

hooks:
  # Host hooks run in the sandbox only if their command starts with one
  # of these prefixes. Project hooks never run.
` + hooks + `
plugins:
  # Host plugins to turn off inside the sandbox, as name@marketplace.
  disable: []
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

// Load reads the config at path, or returns Default if it does not exist.
// Unknown keys are an error so typos don't silently change behaviour.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	return parse(b, path)
}

func parse(b []byte, path string) (Config, error) {
	c := Default()
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	for _, l := range c.Link {
		if l == "" || l == "." || l == ".." || filepath.Base(l) != l {
			return Config{}, fmt.Errorf("%s: link entry %q must be a plain name inside ~/.claude", path, l)
		}
	}
	return c, nil
}

// WriteStarter writes the starter config to path unless a file is already
// there. It reports whether it wrote one.
func WriteStarter(path string, allow []string) (bool, error) {
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
	if _, err := f.WriteString(Starter(allow)); err != nil {
		f.Close()
		return false, err
	}
	return true, f.Close()
}
