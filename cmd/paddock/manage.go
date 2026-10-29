package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/stiyyagura0901/paddock/internal/config"
	"github.com/stiyyagura0901/paddock/internal/sandbox"
	"github.com/stiyyagura0901/paddock/internal/sbx"
)

// ours returns the sandboxes paddock created: those whose name is the one
// paddock derives from their project folder. The rest are counted.
func ours() (mine []sbx.Sandbox, others int, err error) {
	list, err := sbx.List()
	if err != nil {
		return nil, 0, err
	}
	for _, s := range list {
		if len(s.Workspaces) > 0 && s.Name == sandbox.Name(s.Workspaces[0]) {
			mine = append(mine, s)
		} else {
			others++
		}
	}
	return mine, others, nil
}

// find returns the paddock sandbox for a project folder, if there is one.
func find(dir string) (*sbx.Sandbox, error) {
	mine, _, err := ours()
	if err != nil {
		return nil, err
	}
	for i := range mine {
		if mine[i].Workspaces[0] == dir {
			return &mine[i], nil
		}
	}
	return nil, nil
}

func runList() int {
	mine, others, err := ours()
	if err != nil {
		return fail(err)
	}
	if len(mine) == 0 {
		fmt.Println("No paddock sandboxes yet.")
		fmt.Println("Start one in a project folder:  cd ~/code/my-project && paddock")
	} else {
		cfg, cfgErr := loadConfig()
		sort.Slice(mine, func(i, j int) bool { return mine[i].LastUsedAt.After(mine[j].LastUsedAt) })
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "PROJECT\tSTATUS\tLAST USED")
		for _, s := range mine {
			status := s.Status
			if rec := recordedFingerprint(s.Name); cfgErr == nil && rec != "" && rec != currentFingerprint(cfg, s.Workspaces[0]) {
				status += " (older settings)"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", tilde(s.Workspaces[0]), status, ago(s.LastUsedAt))
		}
		w.Flush()
	}
	if others > 0 {
		fmt.Printf("\n%d other sandbox(es) weren't made by paddock; see them with: sbx ls\n", others)
	}
	return 0
}

func runManage(cmd string, args []string) int {
	force := false
	var rest []string
	for _, a := range args {
		switch {
		case cmd == "rm" && (a == "-f" || a == "--force"):
			force = true
		case strings.HasPrefix(a, "-"):
			return fail(fmt.Errorf("unknown option %s. See: paddock help", a))
		default:
			rest = append(rest, a)
		}
	}
	if len(rest) > 1 {
		return fail(fmt.Errorf("%s takes one project folder, got %d", cmd, len(rest)))
	}
	dir, err := projectDir(rest)
	if err != nil {
		return fail(err)
	}
	s, err := find(dir)
	if err != nil {
		return fail(err)
	}
	if s == nil {
		startCmd := "paddock"
		if len(rest) > 0 {
			startCmd += " " + rest[0]
		}
		fmt.Printf("There's no sandbox for %s yet. Start one with: %s\n", tilde(dir), startCmd)
		return 1
	}

	switch cmd {
	case "stop":
		if s.Status == "stopped" {
			fmt.Printf("The sandbox for %s is already stopped.\n", tilde(dir))
			return 0
		}
		if out, err := sbx.Quiet("stop", s.Name); err != nil {
			return fail(fmt.Errorf("couldn't stop the sandbox for %s:\n%s", tilde(dir), out))
		}
		fmt.Printf("Stopped the sandbox for %s. Its conversations are kept; run paddock to carry on.\n", tilde(dir))
		return 0

	case "rm":
		if !force {
			if !interactive() {
				return fail(fmt.Errorf("removing deletes the sandbox's conversations; add --force to do it without asking"))
			}
			if !confirm(fmt.Sprintf("Remove the sandbox for %s? Its conversations will be deleted.", tilde(dir)), false) {
				fmt.Println("Kept it.")
				return 0
			}
		}
		if out, err := sbx.Quiet("rm", "--force", s.Name); err != nil {
			return fail(fmt.Errorf("couldn't remove the sandbox for %s:\n%s", tilde(dir), out))
		}
		os.Remove(recordPath(s.Name))
		fmt.Printf("Removed the sandbox for %s.\n", tilde(dir))
		return 0

	default: // shell
		return exit(sbx.Run("exec", "-it", s.Name, "bash"))
	}
}

var importedRe = regexp.MustCompile(`Imported (\d+) skills?`)

func runSync() int {
	out, err := sbx.Quiet("skills", "import", "--force")
	if err != nil {
		return fail(fmt.Errorf("couldn't import skills:\n%s", out))
	}
	n := "your"
	if m := importedRe.FindStringSubmatch(out); m != nil {
		n = m[1]
	}
	fmt.Printf("Copied %s skills into the sandbox skill store.\n", n)
	fmt.Println("Running sandboxes see new skills after a restart: paddock stop, then paddock.")
	return 0
}

// Each sandbox paddock creates gets a record of the fingerprint it was
// created with, so later runs can tell when it is out of date.
func recordPath(name string) string {
	cache, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(cache, "paddock", "sandboxes", name)
}

func recordedFingerprint(name string) string {
	b, err := os.ReadFile(recordPath(name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func recordFingerprint(name, fp string) error {
	path := recordPath(name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(fp+"\n"), 0o644)
}

// currentFingerprint is what a sandbox created now for project folder dir
// would record.
func currentFingerprint(cfg config.Config, dir string) string {
	home := homeDir()
	return fingerprint(filepath.Join(home, ".claude"), home, cfg.For(dir, home))
}

// staleProjects lists project folders whose sandbox was created with
// different settings than cfg gives it now.
func staleProjects(cfg config.Config) []string {
	mine, _, err := ours()
	if err != nil {
		return nil
	}
	var out []string
	for _, s := range mine {
		if rec := recordedFingerprint(s.Name); rec != "" && rec != currentFingerprint(cfg, s.Workspaces[0]) {
			out = append(out, tilde(s.Workspaces[0]))
		}
	}
	return out
}

func ago(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}
