// Package doctor checks that the host is ready for paddock sandboxes and
// says how to fix what isn't.
package doctor

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stiyyagura0901/paddock/internal/config"
)

// Level is how serious a finding is.
type Level int

const (
	OK Level = iota
	Warn
	Fail
)

// Result is one check's outcome. Fix is a command or hint, empty when OK.
type Result struct {
	Level        Level
	What, Detail string
	Fix          string
}

// Env is what the checks inspect.
type Env struct {
	HostDir    string // host ~/.claude
	ConfigPath string
	Sbx        string // sbx executable
}

// Run performs every check in order.
func Run(e Env) []Result {
	var out []Result
	add := func(r Result) { out = append(out, r) }

	v, err := exec.Command(e.Sbx, "version").Output()
	if err != nil {
		add(Result{Fail, "sbx", "not found or not working", "install Docker Sandboxes: https://docs.docker.com/ai/sandboxes/"})
		return out // everything below needs sbx
	}
	add(Result{Level: OK, What: "sbx", Detail: strings.TrimSpace(firstLine(string(v)))})
	if err := exec.Command(e.Sbx, "ls", "-q").Run(); err != nil {
		add(Result{Fail, "sbx daemon", "sbx ls failed", "run: sbx diagnose"})
	}

	cfg, err := config.Load(e.ConfigPath)
	switch {
	case err != nil:
		add(Result{Fail, "config", err.Error(), "fix the file, or move it aside and run: paddock init"})
		cfg = config.Default()
	case !exists(e.ConfigPath):
		add(Result{Warn, "config", "none; using defaults", "run: paddock init"})
	default:
		add(Result{Level: OK, What: "config", Detail: e.ConfigPath})
	}

	if !exists(e.HostDir) {
		add(Result{Fail, "host config", e.HostDir + " not found", "run Claude Code on the host once"})
	} else {
		var missing []string
		for _, l := range cfg.Link {
			if !exists(filepath.Join(e.HostDir, l)) {
				missing = append(missing, l)
			}
		}
		if len(missing) > 0 {
			add(Result{Warn, "host config", "not found, skipped: " + strings.Join(missing, ", "), "remove them from link: in " + e.ConfigPath})
		} else {
			add(Result{Level: OK, What: "host config", Detail: e.HostDir})
		}
	}

	name, _ := exec.Command("git", "config", "--global", "user.name").Output()
	email, _ := exec.Command("git", "config", "--global", "user.email").Output()
	if strings.TrimSpace(string(name)) == "" || strings.TrimSpace(string(email)) == "" {
		add(Result{Warn, "git identity", "host user.name/user.email not set; sandbox commits will have no author", "git config --global user.name NAME && git config --global user.email EMAIL"})
	} else {
		add(Result{Level: OK, What: "git identity", Detail: "copied into new sandboxes"})
	}

	if os.Getenv("SSH_AUTH_SOCK") == "" || exec.Command("ssh-add", "-l").Run() != nil {
		add(Result{Warn, "ssh agent", "no agent or no keys loaded; git over SSH won't work in sandboxes", "ssh-add"})
	} else {
		add(Result{Level: OK, What: "ssh agent", Detail: "keys loaded; forwarded to sandboxes"})
	}

	secrets, err := exec.Command(e.Sbx, "secret", "ls").Output()
	if err != nil {
		add(Result{Warn, "secrets", "could not list sbx secrets", ""})
		return append(out, skillsResult(e))
	}
	if !globalSecret(strings.NewReader(string(secrets)), "anthropic") {
		add(Result{Warn, "claude login", "no saved Anthropic login; you'll be asked to sign in in every new sandbox", "sbx secret set anthropic"})
	} else {
		add(Result{Level: OK, What: "claude login", Detail: "saved for all sandboxes"})
	}
	if !globalSecret(strings.NewReader(string(secrets)), "github") {
		fix := "sbx secret set github"
		if _, err := exec.LookPath("gh"); err == nil {
			fix = "sbx secret set github --command 'gh auth token'"
		}
		add(Result{Warn, "github token", "no global github secret; gh in sandboxes won't be logged in", fix})
	} else {
		add(Result{Level: OK, What: "github token", Detail: "global sbx secret set"})
	}

	return append(out, skillsResult(e))
}

func skillsResult(e Env) Result {
	missing, err := missingSkills(e)
	switch {
	case err != nil:
		return Result{Warn, "skills", err.Error(), ""}
	case len(missing) > 0:
		return Result{Warn, "skills", "not imported: " + strings.Join(missing, ", "), "paddock sync"}
	default:
		return Result{Level: OK, What: "skills", Detail: "host skills imported"}
	}
}

// Print writes results and reports whether any check failed.
func Print(w io.Writer, rs []Result) (failed bool) {
	for _, r := range rs {
		mark := map[Level]string{OK: "✓", Warn: "!", Fail: "✗"}[r.Level]
		fmt.Fprintf(w, "%s %-13s %s\n", mark, r.What, r.Detail)
		if r.Fix != "" {
			fmt.Fprintf(w, "  %-13s fix: %s\n", "", r.Fix)
		}
		failed = failed || r.Level == Fail
	}
	return failed
}

// globalSecret reports whether `sbx secret ls` output has name in global scope.
func globalSecret(r io.Reader, name string) bool {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) >= 3 && f[0] == "(global)" && f[2] == name {
			return true
		}
	}
	return false
}

// missingSkills lists host skills (directories with SKILL.md) that are not
// in the sbx skill store.
func missingSkills(e Env) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(e.HostDir, "skills"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out, err := exec.Command(e.Sbx, "skills", "ls").Output()
	if err != nil {
		return nil, fmt.Errorf("could not list sbx skills")
	}
	have := storeSkills(strings.NewReader(string(out)))
	var missing []string
	for _, en := range entries {
		if !exists(filepath.Join(e.HostDir, "skills", en.Name(), "SKILL.md")) {
			continue
		}
		if !have[en.Name()] {
			missing = append(missing, en.Name())
		}
	}
	sort.Strings(missing)
	return missing, nil
}

// storeSkills parses `sbx skills ls` output into a set of skill names.
func storeSkills(r io.Reader) map[string]bool {
	have := map[string]bool{}
	sc := bufio.NewScanner(r)
	for first := true; sc.Scan(); first = false {
		f := strings.Fields(sc.Text())
		if first || len(f) == 0 {
			continue // header
		}
		have[f[0]] = true
	}
	return have
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
