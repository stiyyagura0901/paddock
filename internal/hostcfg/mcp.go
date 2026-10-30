package hostcfg

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
)

// MCPServer is one MCP server from the host's ~/.claude.json.
type MCPServer struct {
	Name    string
	Project bool // set up for one project folder rather than for every one
	Type    string
	URL     string
	Command string
	Args    []string
	// Env and Headers name the variables and headers it needs; their
	// values are never read.
	Env, Headers []string
}

type mcpEntry struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	Headers map[string]string `json:"headers"`
}

// MCPServers lists the MCP servers Claude Code on the host has for project
// folder dir: those for every project, and those for dir, which win on a
// name clash as they do in Claude Code. A missing file has none.
func MCPServers(claudeJSON, dir string) ([]MCPServer, error) {
	var f struct {
		MCPServers map[string]mcpEntry `json:"mcpServers"`
		Projects   map[string]struct {
			MCPServers map[string]mcpEntry `json:"mcpServers"`
		} `json:"projects"`
	}
	b, err := os.ReadFile(claudeJSON)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	byName := map[string]MCPServer{}
	for name, e := range f.MCPServers {
		byName[name] = server(name, e, false)
	}
	for p, proj := range f.Projects {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			p = r
		}
		if p != dir {
			continue
		}
		for name, e := range proj.MCPServers {
			byName[name] = server(name, e, true)
		}
	}
	out := make([]MCPServer, 0, len(byName))
	for _, s := range byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func server(name string, e mcpEntry, project bool) MCPServer {
	s := MCPServer{Name: name, Project: project, Type: e.Type, URL: e.URL, Command: e.Command, Args: e.Args,
		Env: keys(e.Env), Headers: keys(e.Headers)}
	if s.Type == "" && s.Command != "" {
		s.Type = "stdio"
	}
	return s
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
