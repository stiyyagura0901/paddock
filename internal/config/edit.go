package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// AddMCP adds MCP server names to the config file at path: to the mcp list
// for every project when project is "", else to the entry for that project
// folder (a key such as ~/code/app), which it creates if needed. Names
// already in that list are left alone. Only the lines it adds change, so
// the file's comments and layout stay as they are.
func AddMCP(path, project string, names []string) error {
	home, _ := os.UserHomeDir()
	return editFile(path, func(src []byte) ([]byte, error) {
		return addMCP(src, project, home, names)
	})
}

// RemoveMCP removes names from the mcp list for every project and from the
// entries for project folder dir. It returns the names it removed.
func RemoveMCP(path, dir string, names []string) ([]string, error) {
	home, _ := os.UserHomeDir()
	var removed []string
	err := editFile(path, func(src []byte) ([]byte, error) {
		out, r, err := removeMCP(src, dir, home, names)
		removed = r
		return out, err
	})
	return removed, err
}

// editFile rewrites the config at path with edit, refusing a result that
// is no longer a valid config.
func editFile(path string, edit func([]byte) ([]byte, error)) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out, err := edit(src)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if string(out) == string(src) {
		return nil
	}
	if _, err := parse(out, path); err != nil {
		return fmt.Errorf("editing %s would break it (%v); change it by hand", path, err)
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

var errByHand = errors.New("its mcp list isn't laid out in a way paddock can edit; change it by hand")

func addMCP(src []byte, project, home string, names []string) ([]byte, error) {
	root, err := rootMap(src)
	if err != nil {
		return nil, err
	}
	keys := []string{"mcp"}
	m := root
	if project != "" {
		keys = []string{"projects", project, "mcp"}
		if projects := value(root, "projects"); projects != nil {
			keys, m = keys[1:], projects
			if entry := projectEntry(projects, project, home); entry != nil {
				keys, m = keys[1:], entry
			}
		}
	}
	var list []string
	if m != nil {
		if v := value(m, "mcp"); v != nil && v.Kind == yaml.SequenceNode {
			for _, n := range v.Content {
				list = append(list, n.Value)
			}
		}
	}
	var add []string
	for _, n := range names {
		if !slices.Contains(list, n) && !slices.Contains(add, n) {
			add = append(add, n)
		}
	}
	if len(add) == 0 {
		return src, nil
	}

	lines := strings.Split(string(src), "\n")
	var out []string
	switch k, v := pair0(m, "mcp"); {
	case m == nil || m == root && (len(keys) > 1 || v == nil):
		block := nested(keys, add, 0)
		// In place of the starter file's commented-out example, if it's there.
		if keys[0] == "mcp" {
			if i := slices.Index(lines, "# mcp:"); i >= 0 {
				j := i + 1
				for j < len(lines) && exampleRe.MatchString(lines[j]) {
					j++
				}
				out = slices.Replace(lines, i, j, block...)
				break
			}
		}
		// Otherwise new top-level keys go at the end of the file.
		text := strings.TrimRight(string(src), "\n")
		if text != "" {
			text += "\n\n"
		}
		out = strings.Split(text+strings.Join(block, "\n")+"\n", "\n")
	case len(keys) > 1 || v == nil:
		// Add the missing keys as the last ones of m.
		if m.Kind != yaml.MappingNode || m.Style&yaml.FlowStyle != 0 || len(m.Content) == 0 {
			return nil, errByHand
		}
		out = slices.Insert(lines, lastLine(m), nested(keys, add, m.Content[0].Column-1)...)
	case v.Kind == yaml.ScalarNode && v.Tag == "!!null":
		lines[k.Line-1] = strings.Repeat(" ", k.Column-1) + "mcp:"
		out = slices.Insert(lines, k.Line, items(add, k.Column-1+2)...)
	case v.Kind == yaml.SequenceNode && v.Style&yaml.FlowStyle != 0:
		if v.Line != k.Line || lastLine(v) != v.Line {
			return nil, errByHand
		}
		var all []string
		for _, n := range append(list, add...) {
			all = append(all, scalar(n))
		}
		lines[v.Line-1] = lines[v.Line-1][:v.Column-1] + "[" + strings.Join(all, ", ") + "]"
		out = lines
	case v.Kind == yaml.SequenceNode:
		out = slices.Insert(lines, lastLine(v), items(add, v.Column-1)...)
	default:
		return nil, errByHand
	}
	res := []byte(strings.Join(out, "\n"))
	err = sameApartFromMCP(src, res, project == "", project, home, true, func(have []string) []string {
		return append(slices.Clone(have), add...)
	})
	return res, err
}

// sameApartFromMCP checks that going from src to res changed nothing but
// what edit does to the global mcp list (if global) and to the list of
// project folder project (if not ""), whose entry create says may be new.
func sameApartFromMCP(src, res []byte, global bool, project, home string, create bool, edit func([]string) []string) error {
	var before, after Config
	if yaml.Unmarshal(src, &before) != nil || yaml.Unmarshal(res, &after) != nil {
		return errByHand
	}
	want := before
	want.Projects = map[string]Project{}
	for k, p := range before.Projects {
		want.Projects[k] = p
	}
	if global {
		want.MCP = edit(before.MCP)
	}
	if project != "" {
		matched := false
		for k, p := range want.Projects {
			if resolve(k, home) == resolve(project, home) {
				p.MCP = edit(p.MCP)
				want.Projects[k] = p
				matched = true
			}
		}
		if !matched && create {
			want.Projects[project] = Project{MCP: edit(nil)}
		}
	}
	if len(want.Projects) == 0 && before.Projects == nil {
		want.Projects = nil
	}
	if !equalConfig(want, after) {
		return errByHand
	}
	return nil
}

// equalConfig compares configs, treating empty and missing lists alike.
func equalConfig(a, b Config) bool {
	norm := func(c Config) string {
		out, _ := yaml.Marshal(c)
		return strings.ReplaceAll(string(out), " []\n", " null\n")
	}
	return norm(a) == norm(b)
}

func removeMCP(src []byte, dir, home string, names []string) ([]byte, []string, error) {
	root, err := rootMap(src)
	if err != nil || root == nil {
		return src, nil, err
	}
	scopes := []*yaml.Node{root}
	if projects := value(root, "projects"); projects != nil && projects.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(projects.Content); i += 2 {
			if resolve(projects.Content[i].Value, home) == dir {
				scopes = append(scopes, projects.Content[i+1])
			}
		}
	}
	lines := strings.Split(string(src), "\n")
	drop := map[int]bool{} // 0-based lines to delete
	var removed []string
	for _, m := range scopes {
		if m.Kind != yaml.MappingNode {
			continue
		}
		k, v := pair(m, "mcp")
		if v == nil || v.Kind != yaml.SequenceNode {
			continue
		}
		var keep []string
		gone := 0
		for _, n := range v.Content {
			if slices.Contains(names, n.Value) {
				gone++
				if !slices.Contains(removed, n.Value) {
					removed = append(removed, n.Value)
				}
			} else {
				keep = append(keep, scalar(n.Value))
			}
		}
		if gone == 0 {
			continue
		}
		if v.Style&yaml.FlowStyle != 0 {
			if v.Line != k.Line || lastLine(v) != v.Line {
				return nil, nil, errByHand
			}
			line := lines[v.Line-1]
			lines[v.Line-1] = line[:v.Column-1] + "[" + strings.Join(keep, ", ") + "]"
			continue
		}
		for _, n := range v.Content {
			if slices.Contains(names, n.Value) {
				if n.Kind != yaml.ScalarNode || n.Line == k.Line {
					return nil, nil, errByHand
				}
				drop[n.Line-1] = true
			}
		}
		if len(keep) == 0 {
			lines[k.Line-1] = strings.Repeat(" ", k.Column-1) + "mcp: []"
		}
	}
	var out []string
	for i, l := range lines {
		if !drop[i] {
			out = append(out, l)
		}
	}
	res := []byte(strings.Join(out, "\n"))
	err = sameApartFromMCP(src, res, true, dir, home, false, func(have []string) []string {
		var keep []string
		for _, n := range have {
			if !slices.Contains(names, n) {
				keep = append(keep, n)
			}
		}
		return keep
	})
	return res, removed, err
}

// rootMap returns the top-level mapping of a YAML file, or nil for a file
// with no keys.
func rootMap(src []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, err
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return nil, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, errors.New("isn't a YAML mapping")
	}
	return root, nil
}

func pair(m *yaml.Node, key string) (k, v *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i], m.Content[i+1]
		}
	}
	return nil, nil
}

func value(m *yaml.Node, key string) *yaml.Node {
	_, v := pair0(m, key)
	return v
}

// pair0 is pair for a node that may be nil or not a mapping.
func pair0(m *yaml.Node, key string) (k, v *yaml.Node) {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil, nil
	}
	return pair(m, key)
}

// projectEntry returns the value under projects for the same folder as
// project, however its key is written.
func projectEntry(projects *yaml.Node, project, home string) *yaml.Node {
	if projects.Kind != yaml.MappingNode {
		return nil
	}
	want := resolve(project, home)
	for i := 0; i+1 < len(projects.Content); i += 2 {
		if resolve(projects.Content[i].Value, home) == want {
			return projects.Content[i+1]
		}
	}
	return nil
}

// lastLine is the 1-based last line n's content starts on.
func lastLine(n *yaml.Node) int {
	l := n.Line
	for _, c := range n.Content {
		l = max(l, lastLine(c))
	}
	return l
}

// nested renders keys as nested block mappings ending in a list of names.
func nested(keys, names []string, indent int) []string {
	var out []string
	for i, k := range keys {
		out = append(out, strings.Repeat(" ", indent+2*i)+scalar(k)+":")
	}
	return append(out, items(names, indent+2*len(keys))...)
}

func items(names []string, indent int) []string {
	var out []string
	for _, n := range names {
		out = append(out, strings.Repeat(" ", indent)+"- "+scalar(n))
	}
	return out
}

var exampleRe = regexp.MustCompile(`^#\s+- \S+$`)

var plainRe = regexp.MustCompile(`^[A-Za-z0-9~/][A-Za-z0-9._/~-]*$`)

// scalar writes s as a YAML scalar, quoted unless it is plainly safe.
func scalar(s string) string {
	if plainRe.MatchString(s) && s != "~" {
		return s
	}
	return strconv.Quote(s)
}
