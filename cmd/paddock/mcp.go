package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/stiyyagura0901/paddock/internal/config"
	"github.com/stiyyagura0901/paddock/internal/doctor"
	"github.com/stiyyagura0901/paddock/internal/hostcfg"
	"github.com/stiyyagura0901/paddock/internal/sbx"
)

const mcpUsage = "Use: paddock mcp, paddock mcp add [--here] NAME..., or paddock mcp rm NAME..."

func runMCP(args []string) int {
	sub := "ls"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "ls":
		if len(args) > 0 {
			return fail(errors.New(mcpUsage))
		}
		return mcpList()
	case "add", "rm":
		here := false
		var names []string
		for _, a := range args {
			switch {
			case a == "--here" && sub == "add":
				here = true
			case strings.HasPrefix(a, "-"):
				return fail(fmt.Errorf("unknown option %s. %s", a, mcpUsage))
			default:
				names = append(names, a)
			}
		}
		if len(names) == 0 {
			return fail(fmt.Errorf("name the MCP servers to %s, for example: paddock mcp %[1]s linear. See them with: paddock mcp", sub))
		}
		if sub == "add" {
			return mcpAdd(names, here)
		}
		return mcpRemove(names)
	}
	return fail(fmt.Errorf("unknown command mcp %s. %s", sub, mcpUsage))
}

// mcpContext is what every mcp command looks at: this folder (if it can
// be a project), the config, and the servers on the host and in sbx.
type mcpContext struct {
	dir, path  string
	cfg        config.Config
	host       map[string]hostcfg.MCPServer
	hostOrder  []string
	registered map[string]bool
}

func loadMCPContext() (mcpContext, error) {
	var c mcpContext
	var err error
	c.dir, _ = projectDir(nil) // "" in the home folder, where only global settings apply
	if c.path, err = config.Path(); err != nil {
		return c, err
	}
	if c.cfg, err = config.Load(c.path); err != nil {
		return c, err
	}
	servers, err := hostcfg.MCPServers(filepath.Join(homeDir(), ".claude.json"), c.dir)
	if err != nil {
		return c, fmt.Errorf("reading ~/.claude.json: %w", err)
	}
	c.host = map[string]hostcfg.MCPServer{}
	for _, s := range servers {
		c.host[s.Name] = s
		c.hostOrder = append(c.hostOrder, s.Name)
	}
	c.registered, err = sbx.MCPServers()
	return c, err
}

// on returns the servers sandboxes of this folder get.
func (c mcpContext) on() []string {
	return c.cfg.For(c.dir, homeDir()).MCP
}

func mcpList() int {
	c, err := loadMCPContext()
	if err != nil {
		return fail(err)
	}
	on := c.on()
	if len(on)+len(c.host) == 0 {
		fmt.Println("Claude Code on this machine has no MCP servers (in ~/.claude.json), and your paddock config turns none on.")
		return 0
	}
	if c.dir != "" {
		fmt.Printf("MCP servers for sandboxes of %s:\n\n", tilde(c.dir))
	} else {
		fmt.Print("MCP servers for sandboxes:\n\n")
	}
	for _, n := range on {
		note := ""
		if !contains(c.cfg.MCP, n) {
			note = " (this project)"
		}
		if !c.registered[n] {
			note += ", but it isn't set up yet: paddock mcp add " + n
		}
		fmt.Printf("  ✓ %-16s on%s\n", n, note)
	}
	for _, n := range c.hostOrder {
		if contains(on, n) {
			continue
		}
		if _, err := sbxAddArgs(c.host[n]); err != nil && !c.registered[n] {
			fmt.Printf("  ✗ %-16s off; paddock can't bring it over: %v\n", n, err)
		} else {
			fmt.Printf("    %-16s off\n", n)
		}
	}
	fmt.Println("\nTurn one on, in running sandboxes too: paddock mcp add NAME (--here for this project only)")
	fmt.Println("Turn one off: paddock mcp rm NAME")
	return 0
}

func mcpAdd(names []string, here bool) int {
	c, err := loadMCPContext()
	if err != nil {
		return fail(err)
	}
	if here && c.dir == "" {
		_, err := projectDir(nil)
		return fail(err)
	}
	if wrote, err := config.WriteStarter(c.path, nil, nil, nil); err != nil {
		return fail(err)
	} else if wrote {
		fmt.Println("Saved a config to", tilde(c.path), "(paddock init offers more settings).")
	}
	on := c.on()
	var global, project []string
	for _, n := range names {
		if !c.registered[n] {
			s, ok := c.host[n]
			if !ok {
				return fail(fmt.Errorf("Claude Code on this machine has no MCP server called %s, and sbx doesn't know one either. See them with: paddock mcp", n))
			}
			args, err := sbxAddArgs(s)
			if err != nil {
				return fail(fmt.Errorf("can't bring %s over: %v", n, err))
			}
			fmt.Printf("Setting up %s for sandboxes (a browser may open to sign in)...\n", n)
			if code, err := sbx.Run(append([]string{"mcp", "add", n}, args...)...); err != nil || code != 0 {
				return exit(1, fmt.Errorf("sbx couldn't set up %s; see above", n))
			}
		}
		switch {
		case contains(on, n):
			fmt.Printf("%s is already on.\n", n)
		case here || c.host[n].Project:
			project = append(project, n)
		default:
			global = append(global, n)
		}
	}
	if len(global) > 0 {
		if err := config.AddMCP(c.path, "", global); err != nil {
			return fail(err)
		}
		fmt.Printf("Turned on %s for every sandbox.\n", strings.Join(global, ", "))
	}
	if len(project) > 0 {
		if err := config.AddMCP(c.path, tilde(c.dir), project); err != nil {
			return fail(err)
		}
		fmt.Printf("Turned on %s for sandboxes of %s.\n", strings.Join(project, ", "), tilde(c.dir))
	}
	return syncAll()
}

func mcpRemove(names []string) int {
	c, err := loadMCPContext()
	if err != nil {
		return fail(err)
	}
	if _, err := os.Stat(c.path); err != nil {
		fmt.Println("paddock has no config yet, so no MCP servers are on.")
		return 0
	}
	removed, err := config.RemoveMCP(c.path, c.dir, names)
	if err != nil {
		return fail(err)
	}
	cfg, err := config.Load(c.path)
	if err != nil {
		return fail(err)
	}
	stillOn := cfg.For(c.dir, homeDir()).MCP
	for _, n := range names {
		switch {
		case contains(stillOn, n):
			fmt.Printf("%s comes from your base config (%s), so it stays on; remove it there.\n", n, cfg.Extends)
		case !contains(removed, n):
			fmt.Printf("%s wasn't on.\n", n)
		}
	}
	if len(removed) > 0 {
		fmt.Printf("Turned off %s. It stays set up in sbx; to remove that too: sbx mcp rm --force NAME\n", strings.Join(removed, ", "))
	}
	return syncAll()
}

// syncAll brings every paddock sandbox's MCP servers in line with the
// config, and says what changed.
func syncAll() int {
	cfg, err := loadConfig()
	if err != nil {
		return fail(err)
	}
	mine, _, err := ours()
	if err != nil {
		return fail(err)
	}
	home := homeDir()
	failed := false
	for _, s := range mine {
		loaded, unloaded, problems := syncMCP(s.Name, cfg.For(s.Workspaces[0], home).MCP)
		what := label(tilde(s.Workspaces[0]), s.named)
		if len(loaded) > 0 {
			fmt.Printf("Added %s to the %s.\n", strings.Join(loaded, ", "), what)
		}
		if len(unloaded) > 0 {
			fmt.Printf("Removed %s from the %s.\n", strings.Join(unloaded, ", "), what)
		}
		for _, p := range problems {
			fmt.Fprintf(os.Stderr, "paddock: %s: %s\n", what, p)
			failed = true
		}
	}
	if failed {
		return 1
	}
	return 0
}

// syncMCP attaches the servers in want to sandbox name that paddock hasn't
// attached yet, and detaches those it attached that want no longer has. A
// running Claude sees the change without a restart.
func syncMCP(name string, want []string) (loaded, unloaded, problems []string) {
	have := attachedMCP(name)
	var now []string
	for _, m := range want {
		if contains(have, m) {
			now = append(now, m)
			continue
		}
		out, err := sbx.Quiet("mcp", "load", m, "--sandbox", name)
		if err != nil {
			if strings.Contains(out, "not registered") {
				problems = append(problems, fmt.Sprintf("MCP server %s isn't set up; run: paddock mcp add %s", m, m))
			} else {
				problems = append(problems, fmt.Sprintf("couldn't add MCP server %s:\n%s", m, out))
			}
			continue
		}
		loaded = append(loaded, m)
		now = append(now, m)
	}
	for _, m := range have {
		if contains(want, m) {
			continue
		}
		if out, err := sbx.Quiet("mcp", "unload", m, "--sandbox", name); err != nil && !strings.Contains(out, "not loaded") {
			problems = append(problems, fmt.Sprintf("couldn't remove MCP server %s:\n%s", m, out))
			now = append(now, m) // try again next time
			continue
		}
		unloaded = append(unloaded, m)
	}
	if strings.Join(now, " ") != strings.Join(have, " ") {
		if err := recordMCP(name, now); err != nil {
			problems = append(problems, err.Error())
		}
	}
	return loaded, unloaded, problems
}

// sbxAddArgs returns the sbx mcp add arguments that set up host server s,
// or why paddock can't do that for you.
func sbxAddArgs(s hostcfg.MCPServer) ([]string, error) {
	switch s.Type {
	case "http", "sse":
		if len(s.Headers) > 0 {
			return nil, fmt.Errorf("it sends headers (%s), which usually hold a token. Set it up with sbx mcp add --header so the token goes into sbx's secret store; see sbx mcp add --help", strings.Join(s.Headers, ", "))
		}
		if s.URL == "" {
			return nil, errors.New("it has no url")
		}
		return []string{"--url", s.URL}, nil
	case "stdio":
		if len(s.Env) > 0 {
			return nil, fmt.Errorf("it needs environment variables (%s), which sbx can't give a server", strings.Join(s.Env, ", "))
		}
		if s.Command == "" {
			return nil, errors.New("it has no command")
		}
		a := []string{"--command", s.Command}
		for _, arg := range s.Args {
			a = append(a, "--args", csvField(arg)) // sbx splits --args on commas
		}
		return a, nil
	}
	return nil, fmt.Errorf("it's a %q server, which sbx doesn't run", s.Type)
}

func csvField(s string) string {
	if s != "" && !strings.ContainsAny(s, ",\"\r\n") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// mcpResult is the doctor line for MCP servers: problems with the ones
// turned on, or which host servers sandboxes could also use.
func mcpResult(cfg config.Config) (doctor.Result, bool) {
	servers, err := hostcfg.MCPServers(filepath.Join(homeDir(), ".claude.json"), "")
	if err != nil {
		return doctor.Result{Level: doctor.Warn, What: "mcp", Detail: "couldn't read ~/.claude.json: " + err.Error()}, true
	}
	registered, err := sbx.MCPServers()
	if err != nil {
		return doctor.Result{Level: doctor.Warn, What: "mcp", Detail: "could not list sbx MCP servers"}, true
	}
	var missing, off []string
	for _, n := range cfg.MCP {
		if !registered[n] {
			missing = append(missing, n)
		}
	}
	for _, s := range servers {
		if _, err := sbxAddArgs(s); err == nil && !contains(cfg.MCP, s.Name) {
			off = append(off, s.Name)
		}
	}
	switch {
	case len(missing) > 0:
		return doctor.Result{Level: doctor.Warn, What: "mcp", Detail: "on but not set up: " + strings.Join(missing, ", "),
			Fix: "paddock mcp add " + strings.Join(missing, " ")}, true
	case len(off) > 0:
		detail := "sandboxes could also use: " + strings.Join(off, ", ") + " (paddock mcp add NAME)"
		if len(cfg.MCP) > 0 {
			detail = "on: " + strings.Join(cfg.MCP, ", ") + "; " + detail
		}
		return doctor.Result{Level: doctor.OK, What: "mcp", Detail: detail}, true
	case len(cfg.MCP) > 0:
		return doctor.Result{Level: doctor.OK, What: "mcp", Detail: "on: " + strings.Join(cfg.MCP, ", ")}, true
	}
	return doctor.Result{}, false
}

// offerMCP asks about each host MCP server that sandboxes could use, sets
// up the ones wanted with sbx, and returns their names.
func offerMCP() []string {
	servers, err := hostcfg.MCPServers(filepath.Join(homeDir(), ".claude.json"), "")
	if err != nil {
		return nil
	}
	var usable []hostcfg.MCPServer
	for _, s := range servers {
		if _, err := sbxAddArgs(s); err == nil {
			usable = append(usable, s)
		}
	}
	if len(usable) == 0 {
		return nil
	}
	if !interactive() {
		fmt.Printf("Claude Code on this machine has %d MCP servers sandboxes could use. Turn them on with: paddock mcp add NAME\n\n", len(usable))
		return nil
	}
	fmt.Println("Sandboxes can also use your MCP servers. Ones that run a command run it on this machine.")
	registered, _ := sbx.MCPServers()
	var names []string
	for _, s := range usable {
		where := s.URL
		if s.Type == "stdio" {
			where = strings.Join(append([]string{s.Command}, s.Args...), " ")
		}
		fmt.Printf("\n  %s  %s\n", s.Name, oneLine(where, 60))
		if !confirm("  Use it in sandboxes?", false) {
			continue
		}
		if !registered[s.Name] {
			args, _ := sbxAddArgs(s)
			if code, err := sbx.Run(append([]string{"mcp", "add", s.Name}, args...)...); err != nil || code != 0 {
				fmt.Printf("  Couldn't set it up; try again later with: paddock mcp add %s\n", s.Name)
				continue
			}
		}
		names = append(names, s.Name)
	}
	fmt.Println()
	return names
}
