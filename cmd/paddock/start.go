package main

import (
	"fmt"
	"os"
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
	cfg                        config.Config
	noConfig                   bool
	exists, stale              bool
	mounts                     []string
	claudeArgs                 []string
}

func (p plan) sbxArgs() []string {
	if p.exists {
		return append([]string{"run", "--name", p.name}, p.claudeArgs...)
	}
	a := []string{"run", "--name", p.name, "--kit", p.kitDir, sandbox.Agent, p.dir}
	for _, m := range p.mounts {
		a = append(a, m+":ro")
	}
	return append(a, p.claudeArgs...)
}

func start(args []string) int {
	dryRun, verbose := false, false
	for len(args) > 0 && (args[0] == "--dry-run" || args[0] == "-v" || args[0] == "--verbose") {
		if args[0] == "--dry-run" {
			dryRun = true
		} else {
			verbose = true
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
	var p plan
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
	if p.cfg, err = config.Load(path); err != nil {
		return fail(err)
	}
	p.name = sandbox.Name(p.dir)
	p.hostDir = filepath.Join(home, ".claude")
	if p.kitDir, err = kitDir(); err != nil {
		return fail(err)
	}
	p.mounts = hostMounts(p.hostDir, p.cfg)
	if sbx.Installed() {
		if p.exists, err = sbx.Exists(p.name); err != nil {
			return fail(err)
		}
	}

	opts := kitOptions(p.hostDir, p.cfg)
	fp := kit.Fingerprint(opts, p.mounts)
	rec := recordedFingerprint(p.name)
	p.stale = p.exists && rec != "" && rec != fp
	if dryRun {
		return show(p, verbose)
	}
	if p.stale {
		fmt.Fprintln(os.Stderr, "paddock: this sandbox was created with older settings. To apply your current ones,")
		fmt.Fprintln(os.Stderr, "  run paddock rm, then paddock (this deletes its conversations).")
	}
	if !p.exists {
		if err := writeKit(p.kitDir, opts); err != nil {
			return fail(err)
		}
		if err := recordFingerprint(p.name, fp); err != nil {
			return fail(err)
		}
		fmt.Printf("Creating a sandbox for %s. The first start takes a little longer.\n", tilde(p.dir))
	}
	return exit(sbx.Run(p.sbxArgs()...))
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
		fmt.Printf("paddock would rejoin the existing sandbox for %s.\n", filepath.Base(p.dir))
		if p.stale {
			fmt.Println("It was created with older settings. Run paddock rm, then paddock, to apply")
			fmt.Println("the ones below (this deletes its conversations).")
		} else {
			fmt.Println("It keeps the settings it was created with, shown below.")
		}
	} else {
		fmt.Printf("paddock would create a new sandbox for %s.\n", filepath.Base(p.dir))
	}
	if p.noConfig {
		fmt.Println("You haven't set paddock up yet, so these are the defaults. paddock init sets it up.")
	}
	fmt.Println()
	fmt.Printf("  Sandbox   %s\n", p.name)
	fmt.Printf("  Project   %s  (Claude can edit)\n", tilde(p.dir))
	var names []string
	for _, m := range p.mounts {
		names = append(names, filepath.Base(m))
	}
	fmt.Printf("  Config    %s: %s  (read-only)\n", tilde(p.hostDir), strings.Join(names, ", "))

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
		fmt.Println("  Command   sbx " + strings.Join(p.sbxArgs(), " "))
	}
	fmt.Println()
	fmt.Println("Nothing was started. Run paddock without --dry-run to start it.")
	return 0
}
