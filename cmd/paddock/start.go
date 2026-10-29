package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/stiyyagura0901/paddock/internal/config"
	"github.com/stiyyagura0901/paddock/internal/hostcfg"
	"github.com/stiyyagura0901/paddock/internal/kit"
	"github.com/stiyyagura0901/paddock/internal/sandbox"
	"github.com/stiyyagura0901/paddock/internal/sbx"
)

// plan is everything paddock needs to start or rejoin a sandbox.
type plan struct {
	name, dir, hostDir, kitDir string
	base                       string // the config's extends, for display
	cfg                        config.Config
	noConfig                   bool
	exists, stale              bool
	mounts                     []string // host config, mounted read-only
	extra                      []string // cfg.Mounts with ~ expanded
	claudeArgs                 []string
	rm                         bool   // remove the sandbox when Claude exits
	review                     string // the PR under review, for display
	prompt                     string // the prompt paddock gives Claude, for display
	command                    string // the paddock command that starts it, for display
	named                      string // set for a named sandbox, which works on a clone
	ref                        string // branch or commit a new named sandbox's clone starts from
}

func (p plan) sbxArgs() []string {
	a := []string{"run", "--name", p.name}
	if p.rm {
		a = append(a, "--rm")
	}
	if !p.exists {
		a = append(a, "--kit", p.kitDir)
		if p.named != "" {
			a = append(a, "--clone")
		}
	}
	// sbx applies -e on re-attach too; mounts and MCP only on create.
	for _, e := range p.cfg.Env {
		a = append(a, "-e", e)
	}
	if p.exists {
		return append(a, p.claudeArgs...)
	}
	for _, m := range p.cfg.MCP {
		a = append(a, "--static-mcp", m)
	}
	a = append(a, sandbox.Agent, p.dir)
	for _, m := range p.mounts {
		a = append(a, m+":ro")
	}
	a = append(a, p.extra...)
	return append(a, p.claudeArgs...)
}

func start(args []string) int {
	dryRun, verbose := false, false
	var p plan
loop:
	for len(args) > 0 {
		if name, used, err := nameArg(args); err != nil {
			return fail(err)
		} else if used > 0 {
			p.named, args = name, args[used:]
			continue
		}
		switch args[0] {
		case "--dry-run":
			dryRun = true
		case "-v", "--verbose":
			verbose = true
		default:
			break loop
		}
		args = args[1:]
	}
	dir := "."
	if len(args) > 0 && args[0] != "--" {
		if strings.HasPrefix(args[0], "-") {
			return fail(fmt.Errorf("unknown option %s. See: paddock help", args[0]))
		}
		dir, args = args[0], args[1:]
	}
	if len(args) > 0 {
		if args[0] != "--" {
			return fail(fmt.Errorf("unexpected %q. Arguments for Claude go after --, for example: paddock -- --continue", args[0]))
		}
		p.claudeArgs = args
	}

	home := homeDir()
	var err error
	if p.dir, err = sandbox.ResolveDir(dir, home); err != nil {
		return fail(err)
	}
	if !dryRun && !sbx.Installed() {
		return fail(errNoSbx)
	}

	path, err := config.Path()
	if err != nil {
		return fail(err)
	}
	if _, err := os.Stat(path); err != nil {
		p.noConfig = true
		// First run: set up before starting, if someone can answer.
		if !dryRun && interactive() {
			fmt.Println("Welcome to paddock. First, a one-time setup.")
			fmt.Println()
			if code := setup(true); code != 0 {
				return code
			}
			fmt.Println()
			p.noConfig = false
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		return fail(err)
	}
	p.cfg, p.base = cfg.For(p.dir, home), cfg.Extends
	p.name = sandbox.Name(p.dir)
	if p.named != "" {
		p.name = sandbox.NamedName(p.dir, p.named)
		p.command = "paddock --name " + p.named
	}
	if err := p.fill(home); err != nil {
		return fail(err)
	}
	if p.named != "" && !p.exists {
		if p.ref, err = cloneable(p.dir); err != nil {
			return fail(err)
		}
	}

	opts := kitOptions(p.hostDir, p.cfg)
	fp := fingerprint(p.hostDir, home, p.cfg)
	rec := recordedFingerprint(p.name)
	p.stale = p.exists && rec != "" && rec != fp
	if dryRun {
		return show(p, verbose)
	}
	if p.stale {
		fmt.Fprintln(os.Stderr, "paddock: this sandbox was created with older settings. To apply your current ones,")
		fmt.Fprintf(os.Stderr, "  run paddock rm%[1]s, then paddock%[1]s (this deletes its conversations).\n", nameFlag(p.named))
	}
	if !p.exists {
		if err := writeKit(p.kitDir, opts); err != nil {
			return fail(err)
		}
		if err := recordFingerprint(p.name, fp); err != nil {
			return fail(err)
		}
		if p.named != "" {
			fmt.Printf("Starting sandbox %q on its own clone of %s (branch/ref: %s). Its commits reach this repo with: git fetch sandbox-%s\n",
				p.named, tilde(p.dir), p.ref, p.name)
		} else {
			fmt.Printf("Creating a sandbox for %s. The first start takes a little longer.\n", tilde(p.dir))
		}
	}
	code, err := sbx.Run(p.sbxArgs()...)
	if p.named != "" && err == nil {
		fetchHint(p)
	}
	return exit(code, err)
}

// nameArg reads --name NAME, -n NAME or --name=NAME at the start of args.
// used is how many arguments it took, 0 if args doesn't start with one.
func nameArg(args []string) (name string, used int, err error) {
	switch {
	case args[0] == "--name" || args[0] == "-n":
		if len(args) < 2 {
			return "", 0, fmt.Errorf("%s needs a name, for example: paddock --name fix-login", args[0])
		}
		name, used = args[1], 2
	case strings.HasPrefix(args[0], "--name="):
		name, used = strings.TrimPrefix(args[0], "--name="), 1
	default:
		return "", 0, nil
	}
	name, err = sandbox.CheckName(name)
	return name, used, err
}

// nameFlag is the --name option that picks a named sandbox, for messages.
func nameFlag(named string) string {
	if named == "" {
		return ""
	}
	return " --name " + named
}

// cloneable checks that sbx can make a clone of dir for a named sandbox,
// and returns the branch, or short commit, the clone will start from.
func cloneable(dir string) (string, error) {
	git := func(args ...string) (string, error) {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
		return strings.TrimSpace(string(out)), err
	}
	abs := func(p string) string {
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return filepath.Clean(p)
	}
	top, err := git("rev-parse", "--show-toplevel")
	if err != nil || abs(top) != dir {
		return "", errors.New("--name needs DIR to be the top folder of a git repository, because each named sandbox works on its own clone of it.")
	}
	gitDir, err1 := git("rev-parse", "--git-dir")
	common, err2 := git("rev-parse", "--git-common-dir")
	if err1 != nil || err2 != nil || abs(gitDir) != abs(common) {
		return "", errors.New("--name doesn't work from a git worktree; run it from the main checkout.")
	}
	if ref, err := git("symbolic-ref", "--short", "-q", "HEAD"); err == nil && ref != "" {
		return ref, nil
	}
	if ref, err := git("rev-parse", "--short", "HEAD"); err == nil {
		return ref, nil
	}
	return "HEAD", nil
}

// fetchHint tells how to bring a named sandbox's commits back, which
// needs the sandbox running.
func fetchHint(p plan) {
	list, err := sbx.List()
	if err != nil {
		return
	}
	fetch := "git fetch sandbox-" + p.name
	if wd, err := os.Getwd(); err != nil || !sameDir(wd, p.dir) {
		fetch = fmt.Sprintf("git -C %s fetch sandbox-%s", tilde(p.dir), p.name)
	}
	for _, s := range list {
		if s.Name != p.name {
			continue
		}
		if s.Status == "running" {
			fmt.Printf("Bring its work back with: %s\n", fetch)
		} else {
			fmt.Printf("Its sandbox is %s. To bring its work back, start it with paddock --name %s, then: %s\n", s.Status, p.named, fetch)
		}
	}
}

func sameDir(a, b string) bool {
	if r, err := filepath.EvalSymlinks(a); err == nil {
		a = r
	}
	return a == b
}

// fill completes a plan whose name, dir and cfg are set.
func (p *plan) fill(home string) error {
	var err error
	p.hostDir = filepath.Join(home, ".claude")
	if p.kitDir, err = kitDir(); err != nil {
		return err
	}
	p.mounts = hostMounts(p.hostDir, p.cfg)
	p.extra = extraMounts(p.cfg, home)
	if sbx.Installed() {
		p.exists, err = sbx.Exists(p.name)
	}
	return err
}

// hostMounts lists the host config paths mounted read-only into sandboxes.
func hostMounts(hostDir string, cfg config.Config) []string {
	var out []string
	for _, e := range append(cfg.Link, "settings.json") {
		if _, err := os.Stat(filepath.Join(hostDir, e)); err == nil {
			out = append(out, filepath.Join(hostDir, e))
		}
	}
	return out
}

// extraMounts returns cfg.Mounts with ~ expanded, keeping any ":ro".
func extraMounts(cfg config.Config, home string) []string {
	var out []string
	for _, m := range cfg.Mounts {
		out = append(out, config.ExpandHome(m, home))
	}
	return out
}

// fingerprint is what a sandbox created now with cfg (already narrowed to
// its project with For) records. It covers everything fixed at creation.
func fingerprint(hostDir, home string, cfg config.Config) string {
	mounts := append(hostMounts(hostDir, cfg), extraMounts(cfg, home)...)
	for _, e := range cfg.Env {
		mounts = append(mounts, "env:"+e)
	}
	for _, m := range cfg.MCP {
		mounts = append(mounts, "mcp:"+m)
	}
	return kit.Fingerprint(kitOptions(hostDir, cfg), mounts)
}

// show prints what starting the sandbox would do, most important first.
func show(p plan, verbose bool) int {
	settings := filepath.Join(p.hostDir, "settings.json")
	hooks, err := hostcfg.Hooks(settings, p.cfg.Hooks.Allow)
	if err != nil {
		return fail(err)
	}
	plugins, err := hostcfg.Plugins(settings)
	if err != nil {
		return fail(err)
	}

	if p.exists {
		fmt.Printf("paddock would rejoin the existing %s.\n", label(filepath.Base(p.dir), p.named))
		if p.stale {
			fmt.Printf("It was created with older settings. Run paddock rm%[1]s, then paddock%[1]s, to apply\n", nameFlag(p.named))
			fmt.Println("the ones below (this deletes its conversations).")
		} else {
			fmt.Println("It keeps the settings it was created with, shown below.")
		}
	} else {
		fmt.Printf("paddock would create a new %s.\n", label(filepath.Base(p.dir), p.named))
	}
	if p.noConfig {
		fmt.Println("You haven't set paddock up yet, so these are the defaults. paddock init sets it up.")
	}
	fmt.Println()
	fmt.Printf("  Sandbox   %s\n", p.name)
	if p.named != "" {
		fmt.Printf("  Project   %s  (read-only; Claude edits its own clone)\n", tilde(p.dir))
		fmt.Printf("  Name      %s (own clone of the repo)\n", p.named)
	} else {
		fmt.Printf("  Project   %s  (Claude can edit)\n", tilde(p.dir))
	}
	if p.review != "" {
		fmt.Printf("  Review    %s\n", p.review)
	}
	if p.base != "" {
		fmt.Printf("  Base      %s\n", p.base)
	}
	var names []string
	for _, m := range p.mounts {
		names = append(names, filepath.Base(m))
	}
	fmt.Printf("  Config    %s: %s  (read-only)\n", tilde(p.hostDir), strings.Join(names, ", "))
	for _, m := range p.extra {
		if path, ok := strings.CutSuffix(m, ":ro"); ok {
			fmt.Printf("  Also      %s  (read-only)\n", tilde(path))
		} else {
			fmt.Printf("  Also      %s  (Claude can edit)\n", tilde(m))
		}
	}
	if len(p.cfg.Env) > 0 {
		var keys []string
		for _, e := range p.cfg.Env {
			k, _, _ := strings.Cut(e, "=")
			keys = append(keys, k) // never the value
		}
		fmt.Printf("  Env       %s\n", strings.Join(keys, ", "))
	}
	if len(p.cfg.MCP) > 0 {
		fmt.Printf("  MCP       %s\n", strings.Join(p.cfg.MCP, ", "))
	}

	var allowed, blocked []hostcfg.Hook
	for _, h := range hooks {
		if h.Allowed {
			allowed = append(allowed, h)
		} else {
			blocked = append(blocked, h)
		}
	}
	fmt.Println()
	fmt.Printf("  Hooks     %d of your %d host hooks will run. Project hooks never run.\n", len(allowed), len(hooks))
	printHooks := func(mark string, hs []hostcfg.Hook) {
		for _, h := range hs {
			fmt.Printf("            %s %-17s %s\n", mark, h.Event, oneLine(h.Command, 50))
		}
	}
	printHooks("✓", allowed)
	if verbose {
		printHooks("✗", blocked)
	} else if len(blocked) > 0 {
		fmt.Printf("            ✗ %d blocked (-v to list them)\n", len(blocked))
	}

	var on, off []string
	for _, pl := range plugins {
		short, _, _ := strings.Cut(pl, "@")
		if contains(p.cfg.Plugins.Disable, pl) {
			off = append(off, short)
		} else {
			on = append(on, short)
		}
	}
	fmt.Println()
	if len(on) == 0 {
		fmt.Println("  Plugins   (none)")
	} else {
		fmt.Printf("  Plugins   %s\n", strings.Join(on, ", "))
	}
	if len(off) > 0 {
		fmt.Printf("  Off       %s\n", strings.Join(off, ", "))
	}

	if verbose {
		fmt.Println()
		args := p.sbxArgs()
		for i := 1; i < len(args); i++ {
			if k, _, ok := strings.Cut(args[i], "="); ok && args[i-1] == "-e" {
				args[i] = k + "=…" // keep env values off the screen
			}
		}
		for i, a := range args {
			if strings.ContainsAny(a, " \"'#$&;|<>()*?`\\") {
				args[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
			}
		}
		fmt.Println("  Command   sbx " + strings.Join(args, " "))
	}
	if p.prompt != "" {
		fmt.Println()
		fmt.Printf("  Prompt    %s\n", p.prompt)
	}
	command := p.command
	if command == "" {
		command = "paddock"
	}
	fmt.Println()
	fmt.Printf("Nothing was started. Run %s without --dry-run to start it.\n", command)
	return 0
}
