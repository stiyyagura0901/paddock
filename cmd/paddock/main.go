// Command paddock runs coding agents in Docker Sandboxes with your host
// config mounted read-only.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/stiyyagura0901/paddock/internal/config"
	"github.com/stiyyagura0901/paddock/internal/doctor"
	"github.com/stiyyagura0901/paddock/internal/hostcfg"
	"github.com/stiyyagura0901/paddock/internal/kit"
	"github.com/stiyyagura0901/paddock/internal/sandbox"
	"github.com/stiyyagura0901/paddock/internal/sbx"
)

const usage = `paddock: run Claude Code in a Docker Sandbox with your host config.

Usage:
  paddock [--dry-run] [DIR] [-- AGENT_ARGS...]
                       start or reattach to DIR's sandbox (default: .)
  paddock init         write a starter config, install the kit, check it with sbx
  paddock doctor       check sbx, config, git, ssh, github token and skills
  paddock sync         re-import host skills into the sandbox skill store
  paddock ls           list sandboxes
  paddock stop [DIR]   stop DIR's sandbox
  paddock rm [DIR]     remove DIR's sandbox (deletes its sessions)
  paddock shell [DIR]  open a shell in DIR's sandbox
  paddock version      print the version

Flags:
  --dry-run   show what would be shared and which hooks run, then exit
  -v          with --dry-run, also list blocked hooks and the sbx command

Resume the last conversation with: paddock -- --continue
`

// version is set at release build time.
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "-h", "--help", "help":
			fmt.Print(usage)
			return 0
		case "version", "--version":
			fmt.Println("paddock", version)
			return 0
		case "init":
			return initKit()
		case "doctor":
			return runDoctor()
		case "sync":
			return withSbx(func() int { return exit(sbx.Run("skills", "import", "--force")) })
		case "ls":
			return withSbx(func() int { return exit(sbx.Run("ls")) })
		case "stop", "rm", "shell":
			name, err := nameFor(args[1:])
			if err != nil {
				return fail(err)
			}
			return withSbx(func() int {
				switch args[0] {
				case "stop":
					return exit(sbx.Run("stop", name))
				case "rm":
					code, err := sbx.Run("rm", name)
					if err == nil && code == 0 {
						os.Remove(recordPath(name))
					}
					return exit(code, err)
				default:
					return exit(sbx.Run("exec", "-it", name, "bash"))
				}
			})
		}
	}
	return start(args)
}

// plan is everything paddock needs to start or reattach to a sandbox.
type plan struct {
	name, dir, hostDir, kitDir string
	cfg                        config.Config
	exists, stale              bool
	mounts                     []string
	agentArgs                  []string
}

func (p plan) sbxArgs() []string {
	if p.exists {
		return append([]string{"run", "--name", p.name}, p.agentArgs...)
	}
	a := []string{"run", "--name", p.name, "--kit", p.kitDir, sandbox.Agent, p.dir}
	for _, m := range p.mounts {
		a = append(a, m+":ro")
	}
	return append(a, p.agentArgs...)
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
			return fail(fmt.Errorf("unknown flag %q; see paddock --help", args[0]))
		}
		dir, args = args[0], args[1:]
	}
	p := plan{}
	if len(args) > 0 {
		if args[0] != "--" {
			return fail(fmt.Errorf("unexpected argument %q; agent arguments go after --", args[0]))
		}
		p.agentArgs = args
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err)
	}
	if p.dir, err = sandbox.ResolveDir(dir, home); err != nil {
		return fail(err)
	}
	if p.cfg, err = loadConfig(); err != nil {
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
	} else if !dryRun {
		return fail(errNoSbx)
	}

	opts := kitOptions(p.hostDir, p.cfg)
	fp := kit.Fingerprint(opts, p.mounts)
	p.stale = p.exists && recordedFingerprint(p.name) != "" && recordedFingerprint(p.name) != fp
	if dryRun {
		return show(p, home, verbose)
	}
	if p.stale {
		fmt.Fprintln(os.Stderr, "paddock: your config or paddock changed since this sandbox was created; run paddock rm, then paddock, to apply it (this deletes its sessions)")
	}
	if !p.exists {
		if err := writeKit(p.kitDir, opts); err != nil {
			return fail(err)
		}
		if err := recordFingerprint(p.name, fp); err != nil {
			return fail(err)
		}
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

// staleSandboxes lists existing sandboxes created with a different
// fingerprint than the current one.
func staleSandboxes(current string) []string {
	names, err := sbx.Names()
	if err != nil {
		return nil
	}
	var out []string
	for _, n := range names {
		if fp := recordedFingerprint(n); fp != "" && fp != current {
			out = append(out, n)
		}
	}
	return out
}

// show prints what starting the sandbox would do, most important first.
func show(p plan, home string, verbose bool) int {
	tilde := func(s string) string {
		if rel, err := filepath.Rel(home, s); err == nil && !strings.HasPrefix(rel, "..") {
			return "~/" + rel
		}
		return s
	}
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
		fmt.Printf("paddock would reattach to the existing sandbox for %s.\n", filepath.Base(p.dir))
		if p.stale {
			fmt.Println("It is out of date: your config or paddock changed since it was created.")
			fmt.Println("Run paddock rm, then paddock, to apply the settings below (this deletes its sessions).")
		} else {
			fmt.Println("It keeps the settings it was created with, shown below.")
		}
	} else {
		fmt.Printf("paddock would create a new sandbox for %s.\n", filepath.Base(p.dir))
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
	fmt.Printf("  Plugins   %s\n", orNone(on))
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

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func orNone(s []string) string {
	if len(s) == 0 {
		return "(none)"
	}
	return strings.Join(s, ", ")
}

// oneLine collapses whitespace in s and cuts it to at most n runes.
func oneLine(s string, n int) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}

// initKit is first-run setup: write a config (offering the host hooks that
// will work in a sandbox), install and validate the kit, then run doctor.
func initKit() int {
	if !sbx.Installed() {
		return fail(errNoSbx)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err)
	}
	hostDir := filepath.Join(home, ".claude")
	path, err := config.Path()
	if err != nil {
		return fail(err)
	}

	if _, err := os.Stat(path); err == nil {
		fmt.Println("Keeping your config at", path)
	} else {
		allow, err := offerHooks(hostDir, home)
		if err != nil {
			return fail(err)
		}
		if _, err := config.WriteStarter(path, allow); err != nil {
			return fail(err)
		}
		fmt.Println("Wrote config to", path)
	}

	cfg, err := config.Load(path)
	if err != nil {
		return fail(err)
	}
	dir, err := kitDir()
	if err != nil {
		return fail(err)
	}
	if err := writeKit(dir, kitOptions(hostDir, cfg)); err != nil {
		return fail(err)
	}
	if out, err := exec.Command(sbx.Bin, "kit", "validate", "--offline", dir).CombinedOutput(); err != nil {
		return fail(fmt.Errorf("sbx rejected the kit:\n%s", out))
	}
	fmt.Println("Installed the sandbox kit.")

	fmt.Println("\nChecking your setup:")
	if runDoctor() != 0 {
		fmt.Println("\nFix the ✗ items above, then run paddock init again.")
		return 1
	}
	fmt.Println("\nReady. In any project folder, run: paddock")
	return 0
}

// offerHooks finds host hooks that will work in a sandbox and, on a
// terminal, asks whether to allow them. It returns the prefixes to allow.
func offerHooks(hostDir, home string) ([]string, error) {
	hooks, err := hostcfg.Hooks(filepath.Join(hostDir, "settings.json"), nil)
	if err != nil {
		return nil, err
	}
	var allow []string
	var lines []string
	for _, h := range hooks {
		prefix, ok := hostcfg.Suggest(h.Command, config.Default().Link, home)
		if !ok {
			continue
		}
		lines = append(lines, fmt.Sprintf("  %-17s %s", h.Event, oneLine(h.Command, 50)))
		if !contains(allow, prefix) {
			allow = append(allow, prefix)
		}
	}
	if len(allow) == 0 {
		return nil, nil
	}
	fmt.Printf("%d of your %d host hooks can run inside sandboxes:\n%s\n", len(lines), len(hooks), strings.Join(lines, "\n"))
	notInteractive := "\nNo answer; leaving them off. Add them under hooks.allow later."
	if fi, err := os.Stdin.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		fmt.Println(notInteractive)
		return nil, nil
	}
	fmt.Print("Allow them? [Y/n] ")
	var answer string
	if _, err := fmt.Scanln(&answer); errors.Is(err, io.EOF) { // e.g. stdin is /dev/null
		fmt.Println(notInteractive)
		return nil, nil
	}
	if a := strings.ToLower(strings.TrimSpace(answer)); a != "" && a != "y" && a != "yes" {
		return nil, nil
	}
	return allow, nil
}

// kitDir is where the embedded kit is written before each new sandbox.
func kitDir() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "paddock", "kit"), nil
}

func loadConfig() (config.Config, error) {
	path, err := config.Path()
	if err != nil {
		return config.Config{}, err
	}
	return config.Load(path)
}

func kitOptions(hostDir string, cfg config.Config) kit.Options {
	return kit.Options{
		HostDir:        hostDir,
		Links:          cfg.Link,
		HookPrefixes:   cfg.Hooks.Allow,
		DisablePlugins: cfg.Plugins.Disable,
		GitName:        gitConfig("user.name"),
		GitEmail:       gitConfig("user.email"),
	}
}

func writeKit(dir string, opts kit.Options) error {
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	return kit.Write(dir, opts)
}

func gitConfig(key string) string {
	out, err := exec.Command("git", "config", "--global", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func runDoctor() int {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err)
	}
	path, err := config.Path()
	if err != nil {
		return fail(err)
	}
	hostDir := filepath.Join(home, ".claude")
	results := doctor.Run(doctor.Env{HostDir: hostDir, ConfigPath: path, Sbx: sbx.Bin})
	if cfg, err := config.Load(path); err == nil && sbx.Installed() {
		fp := kit.Fingerprint(kitOptions(hostDir, cfg), hostMounts(hostDir, cfg))
		if stale := staleSandboxes(fp); len(stale) > 0 {
			results = append(results, doctor.Result{Level: doctor.Warn, What: "sandboxes",
				Detail: "created with older settings: " + strings.Join(stale, ", "),
				Fix:    "sbx rm NAME, then paddock in that project (deletes its sessions)"})
		}
	}
	if doctor.Print(os.Stdout, results) {
		return 1
	}
	return 0
}

var errNoSbx = fmt.Errorf("sbx not found on PATH; install Docker Sandboxes first")

func withSbx(f func() int) int {
	if !sbx.Installed() {
		return fail(errNoSbx)
	}
	return f()
}

func nameFor(args []string) (string, error) {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	abs, err := sandbox.ResolveDir(dir, home)
	if err != nil {
		return "", err
	}
	return sandbox.Name(abs), nil
}

func exit(code int, err error) int {
	if err != nil {
		return fail(err)
	}
	return code
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "paddock:", err)
	return 1
}
