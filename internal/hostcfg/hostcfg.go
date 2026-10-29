// Package hostcfg reads the parts of the host Claude settings that paddock
// previews, without ever writing to them.
package hostcfg

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Hook is one host hook command and the event it runs on.
type Hook struct {
	Event   string
	Command string
	Allowed bool
}

type settings struct {
	Hooks map[string][]struct {
		Hooks []struct {
			Command string `json:"command"`
		} `json:"hooks"`
	} `json:"hooks"`
	EnabledPlugins map[string]bool `json:"enabledPlugins"`
}

func load(path string) (settings, error) {
	var s settings
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(b, &s)
}

// Hooks lists every command hook in the settings file at path, marking the
// ones whose command starts with an allowed prefix. A missing file has none.
func Hooks(path string, prefixes []string) ([]Hook, error) {
	s, err := load(path)
	if err != nil {
		return nil, err
	}
	var out []Hook
	for event, groups := range s.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				if h.Command == "" {
					continue
				}
				out = append(out, Hook{Event: event, Command: h.Command, Allowed: hasPrefix(h.Command, prefixes)})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Event < out[j].Event })
	return out, nil
}

// Plugins returns the plugins enabled in the settings file at path, sorted.
func Plugins(path string) ([]string, error) {
	s, err := load(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for p, on := range s.EnabledPlugins {
		if on {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Suggest returns an allow prefix for a host hook that can work inside a
// sandbox, or false. A hook works there if it only prints (echo, printf) or
// runs a script inside a folder of ~/.claude, which the sandbox sees at the
// same path once that folder is linked. needLink names that folder when it
// is not in links yet. Anything else, such as scripts elsewhere on the host
// or long inline shell, is left blocked.
func Suggest(cmd string, links []string, home string) (prefix, needLink string, ok bool) {
	f := strings.Fields(cmd)
	if len(f) == 0 {
		return "", "", false
	}
	if f[0] == "echo" || f[0] == "printf" {
		return f[0] + " ", "", true
	}
	i := 0
	if (f[0] == "bash" || f[0] == "sh" || f[0] == "zsh") && len(f) > 1 {
		i = 1
	}
	script := f[i]
	var rel string
	for _, root := range []string{"~/.claude/", "$HOME/.claude/", home + "/.claude/"} {
		if strings.HasPrefix(script, root) {
			rel = script[len(root):]
			break
		}
	}
	top, rest, _ := strings.Cut(rel, "/")
	if rel == "" || rest == "" {
		return "", "", false // not inside a folder of ~/.claude
	}
	if fi, err := os.Stat(filepath.Join(home, ".claude", top)); err != nil || !fi.IsDir() {
		return "", "", false
	}
	if !contains(links, top) {
		needLink = top
	}
	return strings.Join(f[:i+1], " "), needLink, true
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func hasPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
