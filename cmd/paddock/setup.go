package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/stiyyagura0901/paddock/internal/config"
	"github.com/stiyyagura0901/paddock/internal/doctor"
	"github.com/stiyyagura0901/paddock/internal/hostcfg"
	"github.com/stiyyagura0901/paddock/internal/kit"
	"github.com/stiyyagura0901/paddock/internal/sbx"
)

func runInit() int {
	if !sbx.Installed() {
		return fail(errNoSbx)
	}
	if code := setup(false); code != 0 {
		return code
	}
	fmt.Println("\nReady. In any project folder, run: paddock")
	return 0
}

// setup writes a config if there is none (offering the host hooks that
// can run in a sandbox), installs and validates the kit, then checks the
// rest of the setup. On a first run it only reports problems.
func setup(firstRun bool) int {
	home := homeDir()
	hostDir := filepath.Join(home, ".claude")
	path, err := config.Path()
	if err != nil {
		return fail(err)
	}

	if _, err := os.Stat(path); err == nil {
		fmt.Println("Using your config at", tilde(path))
	} else {
		extra, allow, err := offerHooks(hostDir, home)
		if err != nil {
			return fail(err)
		}
		if _, err := config.WriteStarter(path, extra, allow); err != nil {
			return fail(err)
		}
		fmt.Println("Saved your config to", tilde(path), "(edit it any time).")
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
		return fail(fmt.Errorf("sbx rejected paddock's sandbox kit. Please report this.\n%s", out))
	}

	fmt.Println()
	if firstRun {
		return runDoctor(true)
	}
	fmt.Println("Checking your setup:")
	return runDoctor(false)
}

// candidate is a host hook command (by allow prefix) that could also run in
// sandboxes, with the events it runs on.
type candidate struct {
	prefix, needLink, command string
	events                    []string
	script                    bool
}

// offerHooks finds host hooks that could run in a sandbox and asks about
// each. It returns extra ~/.claude folders to share and the hook prefixes
// to allow.
func offerHooks(hostDir, home string) (extraLinks, allow []string, err error) {
	hooks, err := hostcfg.Hooks(filepath.Join(hostDir, "settings.json"), nil)
	if err != nil {
		return nil, nil, err
	}
	var cands []*candidate
	byPrefix := map[string]*candidate{}
	for _, h := range hooks {
		prefix, needLink, ok := hostcfg.Suggest(h.Command, config.Default().Link, home)
		if !ok {
			continue
		}
		c := byPrefix[prefix]
		if c == nil {
			c = &candidate{prefix: prefix, needLink: needLink, command: h.Command, script: !strings.HasSuffix(prefix, " ")}
			byPrefix[prefix] = c
			cands = append(cands, c)
		}
		if !contains(c.events, h.Event) {
			c.events = append(c.events, h.Event)
		}
	}
	if len(hooks) == 0 {
		return nil, nil, nil
	}
	if len(cands) == 0 {
		fmt.Printf("Your %d host hooks all depend on this machine, so they won't run in sandboxes.\n", len(hooks))
		return nil, nil, nil
	}
	fmt.Printf("You have %d host hooks. The ones below could also run inside sandboxes; the\n", len(hooks))
	fmt.Println("rest depend on this machine and stay off. Project hooks never run either way.")
	if !interactive() {
		fmt.Println("No one to ask, so they all stay off. Add any later under hooks.allow.")
		return nil, nil, nil
	}
	fmt.Println("Scripts run on Linux inside the sandbox, so only say yes if yours works there.")
	for _, c := range cands {
		fmt.Printf("\n  %s\n  on %s\n", oneLine(c.command, 70), strings.Join(c.events, ", "))
		if c.needLink != "" {
			fmt.Printf("  shares ~/.claude/%s with sandboxes, read-only\n", c.needLink)
		}
		if !confirm("  Run it in sandboxes?", !c.script) {
			continue
		}
		allow = append(allow, c.prefix)
		if c.needLink != "" && !contains(extraLinks, c.needLink) {
			extraLinks = append(extraLinks, c.needLink)
		}
	}
	fmt.Println()
	return extraLinks, allow, nil
}

// runDoctor checks the setup. With problemsOnly it prints nothing but a
// short line when all is well.
func runDoctor(problemsOnly bool) int {
	home := homeDir()
	path, err := config.Path()
	if err != nil {
		return fail(err)
	}
	hostDir := filepath.Join(home, ".claude")
	results := doctor.Run(doctor.Env{HostDir: hostDir, ConfigPath: path, Sbx: sbx.Bin})
	if cfg, err := config.Load(path); err == nil && sbx.Installed() {
		if stale := staleProjects(cfg); len(stale) > 0 {
			results = append(results, doctor.Result{Level: doctor.Warn, What: "sandboxes",
				Detail: "created with older settings: " + strings.Join(stale, ", "),
				Fix:    "in that project, run paddock rm, then paddock (deletes its conversations)"})
		}
	}
	if problemsOnly {
		var problems []doctor.Result
		for _, r := range results {
			if r.Level != doctor.OK {
				problems = append(problems, r)
			}
		}
		if len(problems) == 0 {
			fmt.Println("✓ Everything else looks good.")
			return 0
		}
		fmt.Println("A few things to know:")
		results = problems
	}
	if doctor.Print(os.Stdout, results) {
		fmt.Println("\nFix the ✗ items above, then run paddock init again.")
		return 1
	}
	return 0
}

// kitDir is where the embedded kit is written before each new sandbox.
func kitDir() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "paddock", "kit"), nil
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
