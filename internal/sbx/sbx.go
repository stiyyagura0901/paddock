// Package sbx is a thin wrapper around the Docker Sandboxes CLI.
package sbx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Bin is the sbx executable; overridable for tests.
var Bin = "sbx"

// Run runs sbx attached to the terminal and returns its exit code.
func Run(args ...string) (int, error) {
	cmd := exec.Command(Bin, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	if err != nil {
		return 1, fmt.Errorf("running %s: %w", Bin, err)
	}
	return 0, nil
}

// Sandbox is one entry from `sbx ls --json`.
type Sandbox struct {
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Workspaces []string  `json:"workspaces"`
	LastUsedAt time.Time `json:"last_used_at"`
}

// List returns all local sandboxes.
func List() ([]Sandbox, error) {
	out, err := exec.Command(Bin, "ls", "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("%s ls: %w", Bin, err)
	}
	var list []Sandbox
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, nil
	}
	if err := decodeJSON(out, &list); err != nil {
		return nil, fmt.Errorf("%s ls: unexpected output: %w", Bin, err)
	}
	return list, nil
}

// Quiet runs sbx without a terminal and returns its combined output, so
// callers can show their own message instead of sbx's.
func Quiet(args ...string) (string, error) {
	out, err := exec.Command(Bin, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// Names returns the names of all local sandboxes.
func Names() ([]string, error) {
	out, err := exec.Command(Bin, "ls", "-q").Output()
	if err != nil {
		return nil, fmt.Errorf("%s ls: %w", Bin, err)
	}
	var names []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		if n := strings.TrimSpace(sc.Text()); n != "" {
			names = append(names, n)
		}
	}
	return names, sc.Err()
}

// Exists reports whether a sandbox with the given name exists.
func Exists(name string) (bool, error) {
	names, err := Names()
	if err != nil {
		return false, err
	}
	for _, n := range names {
		if n == name {
			return true, nil
		}
	}
	return false, nil
}

// MCPServers returns the names of the MCP servers registered with sbx.
func MCPServers() (map[string]bool, error) {
	out, err := exec.Command(Bin, "mcp", "ls", "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("%s mcp ls: %w", Bin, err)
	}
	var list []struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(out, &list); err != nil {
		return nil, fmt.Errorf("%s mcp ls: unexpected output: %w", Bin, err)
	}
	names := map[string]bool{}
	for _, s := range list {
		names[s.Name] = true
	}
	return names, nil
}

// decodeJSON reads the JSON value at the start of out into v, ignoring
// anything sbx prints after it, such as a notice that an update is out.
func decodeJSON(out []byte, v any) error {
	return json.NewDecoder(bytes.NewReader(out)).Decode(v)
}

// Installed reports whether the sbx executable is on PATH.
func Installed() bool {
	_, err := exec.LookPath(Bin)
	return err == nil
}
