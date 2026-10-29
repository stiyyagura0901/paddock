// Command paddock runs coding agents in Docker Sandboxes with your host
// config mounted read-only.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/stiyyagura0901/paddock/internal/config"
	"github.com/stiyyagura0901/paddock/internal/sandbox"
	"github.com/stiyyagura0901/paddock/internal/sbx"
)

const usage = `paddock: run Claude Code in a Docker Sandbox with your own setup.

Usage:
  paddock [DIR] [-- CLAUDE_ARGS...]
                        start, or rejoin, the sandbox for DIR (default: this folder)
  paddock --name NAME [DIR] [-- CLAUDE_ARGS...]
                        start, or rejoin, another sandbox for DIR, working on its
                        own clone of the git repo, so several can run at once
  paddock --dry-run [-v] [--name NAME] [DIR]
                        show what the sandbox would get, without starting it
  paddock review [--keep] PR [-- CLAUDE_ARGS...]
                        have Claude review a GitHub pull request in a throwaway
                        sandbox with no hooks (PR: URL, OWNER/REPO#N, or N)
  paddock init          set up, or check, paddock (runs on first use too)
  paddock doctor        check your setup and print fixes
  paddock sync          copy your skills into sandboxes
  paddock ls            list your paddock sandboxes
  paddock stop [--name NAME] [DIR]
                        stop DIR's sandbox; its conversations are kept
  paddock rm [-f] [--name NAME] [DIR]
                        remove DIR's sandbox and its conversations
  paddock shell [--name NAME] [DIR]
                        open a shell in DIR's sandbox
  paddock version       print the version

Examples:
  paddock -- --continue      resume the last conversation
  paddock -- --resume        pick a conversation to resume
  paddock --name fix-login   a second session on this repo, on its own clone
`

// version is set at release build time.
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return 0
	case "version", "--version":
		fmt.Println("paddock", versionString())
		return 0
	case "init":
		return runInit()
	case "doctor":
		return runDoctor(false)
	case "sync":
		return withSbx(runSync)
	case "ls":
		return withSbx(runList)
	case "review":
		return runReview(args[1:])
	case "stop", "rm", "shell":
		return withSbx(func() int { return runManage(cmd, args[1:]) })
	}
	return start(args)
}

func versionString() string {
	if version != "dev" {
		return version
	}
	// go install builds carry the module version instead of ldflags.
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return strings.TrimPrefix(info.Main.Version, "v")
	}
	return version
}

func withSbx(f func() int) int {
	if !sbx.Installed() {
		return fail(errNoSbx)
	}
	return f()
}

var errNoSbx = errors.New("Docker Sandboxes (sbx) isn't installed, or isn't on your PATH.\n  Install it: https://docs.docker.com/ai/sandboxes/")

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/"
	}
	return home
}

// tilde shortens paths under the home folder for display.
func tilde(p string) string {
	if rel, err := filepath.Rel(homeDir(), p); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return "~/" + rel
	}
	return p
}

// projectDir resolves the optional DIR argument of a command.
func projectDir(args []string) (string, error) {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	return sandbox.ResolveDir(dir, homeDir())
}

func loadConfig() (config.Config, error) {
	path, err := config.Path()
	if err != nil {
		return config.Config{}, err
	}
	return config.Load(path)
}

// interactive reports whether stdin is a terminal someone can answer from.
func interactive() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// confirm asks a yes/no question. Without a terminal it returns def.
func confirm(question string, def bool) bool {
	if !interactive() {
		return def
	}
	hint := "[y/N]"
	if def {
		hint = "[Y/n]"
	}
	fmt.Printf("%s %s ", question, hint)
	var answer string
	if _, err := fmt.Scanln(&answer); errors.Is(err, io.EOF) {
		fmt.Println()
		return def
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "":
		return def
	case "y", "yes":
		return true
	}
	return false
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// oneLine collapses whitespace in s and cuts it to at most n runes.
func oneLine(s string, n int) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
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
