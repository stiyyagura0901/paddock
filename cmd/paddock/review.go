package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/stiyyagura0901/paddock/internal/config"
	"github.com/stiyyagura0901/paddock/internal/sandbox"
	"github.com/stiyyagura0901/paddock/internal/sbx"
)

// pr is the part of `gh pr view --json` a review needs.
type pr struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	BaseRefName string `json:"baseRefName"`
}

var (
	repoRe  = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	shortRe = regexp.MustCompile(`^([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)#([0-9]+)$`)
)

// parsePR reads a pull request given as a GitHub URL, OWNER/REPO#N or a
// bare number N. repo is "" for a bare number: the current folder's repo.
func parsePR(ref string) (repo string, n int, err error) {
	bad := fmt.Errorf("%q isn't a pull request. Give a URL (https://github.com/OWNER/REPO/pull/N), OWNER/REPO#N, or a number for this folder's repo", ref)
	num := func(s string) (int, error) {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			return 0, bad
		}
		return n, nil
	}
	switch {
	case strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://"):
		u, err := url.Parse(ref)
		if err != nil || (u.Host != "github.com" && u.Host != "www.github.com") {
			return "", 0, bad
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 4 || parts[2] != "pull" || !repoRe.MatchString(parts[0]+"/"+parts[1]) {
			return "", 0, bad
		}
		n, err := num(parts[3])
		return parts[0] + "/" + parts[1], n, err
	case shortRe.MatchString(ref):
		m := shortRe.FindStringSubmatch(ref)
		n, err := num(m[2])
		return m[1], n, err
	default:
		n, err := num(ref)
		return "", n, err
	}
}

// reviewPrompt is what Claude is asked when no arguments are given.
func reviewPrompt(p pr) string {
	return fmt.Sprintf("Review pull request #%d %q (%s). The PR's changes are checked out at HEAD; "+
		"compare against origin/%s with: git diff origin/%s...HEAD. Report bugs, security problems and "+
		"risky changes first, then smaller suggestions, citing file:line. Do not push, comment on the PR, "+
		"or change files outside this folder.", p.Number, p.Title, p.URL, p.BaseRefName, p.BaseRefName)
}

// gh runs the GitHub CLI and returns its trimmed output. On failure the
// error carries what gh printed.
func gh(dir string, args ...string) (string, error) {
	cmd := exec.Command("gh", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		if s == "" {
			s = err.Error()
		}
		return "", errors.New(s)
	}
	return s, nil
}

// hostGit runs git in dir with gh as the credential helper for GitHub, as
// gh does for its own git commands, so private repos fetch too.
func hostGit(dir string, args ...string) (string, error) {
	pre := []string{"-C", dir, "-c", "credential.https://github.com.helper=", "-c", "credential.https://github.com.helper=!gh auth git-credential"}
	out, err := exec.Command("git", append(pre, args...)...).CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		return s, fmt.Errorf("git %s: %s", strings.Join(args, " "), s)
	}
	return s, nil
}

// reviewDir is where the clone for a pull request goes.
func reviewDir(repo string, n int) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	slug := strings.ReplaceAll(repo, "/", "-") + "-pr-" + strconv.Itoa(n)
	return filepath.Join(cache, "paddock", "review", slug), nil
}

// cloneForReview clones repo into dir with the pull request checked out.
// Nothing from the repo runs on this machine: hooks are switched off
// before anything is checked out.
func cloneForReview(dir, repo string, p pr) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	if _, err := gh("", "repo", "clone", repo, dir, "--", "--filter=blob:none", "--no-checkout", "--quiet"); err != nil {
		return fmt.Errorf("couldn't clone %s:\n%v", repo, err)
	}
	if _, err := hostGit(dir, "config", "core.hooksPath", "/dev/null"); err != nil {
		return err
	}
	// refs/pull/N/head outlives the PR's branch, and works for forks.
	head := "refs/remotes/origin/pr/" + strconv.Itoa(p.Number)
	if _, err := hostGit(dir, "fetch", "-q", "origin", "+refs/pull/"+strconv.Itoa(p.Number)+"/head:"+head); err != nil {
		return fmt.Errorf("couldn't fetch pull request #%d:\n%v", p.Number, err)
	}
	if _, err := hostGit(dir, "checkout", "-q", "--detach", head); err != nil {
		return err
	}
	base := "origin/" + p.BaseRefName
	if _, err := hostGit(dir, "rev-parse", "--verify", "-q", base); err != nil {
		if _, err := hostGit(dir, "fetch", "-q", "origin", p.BaseRefName); err != nil {
			return err
		}
	}
	// The clone is partial: fetch the blobs the diff needs now, so the
	// sandbox can diff without reaching GitHub.
	_, err := hostGit(dir, "diff", "--stat", base+"...HEAD")
	return err
}

func runReview(args []string) int {
	dryRun, verbose, keep := false, false, false
	var ref string
	var claudeArgs []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--":
			claudeArgs = args[i:]
			i = len(args)
		case a == "--dry-run":
			dryRun = true
		case a == "-v" || a == "--verbose":
			verbose = true
		case a == "--keep":
			keep = true
		case strings.HasPrefix(a, "-"):
			return fail(fmt.Errorf("unknown option %s. See: paddock help", a))
		case ref != "":
			return fail(fmt.Errorf("review takes one pull request, got %q and %q. Arguments for Claude go after --", ref, a))
		default:
			ref = a
		}
	}
	if ref == "" {
		return fail(errors.New("which pull request? For example: paddock review https://github.com/OWNER/REPO/pull/123"))
	}
	repo, n, err := parsePR(ref)
	if err != nil {
		return fail(err)
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return fail(errors.New("paddock review needs the GitHub CLI (gh), logged in. Install it from https://cli.github.com and run gh auth login."))
	}
	if !dryRun && !sbx.Installed() {
		return fail(errNoSbx)
	}
	if repo == "" {
		if repo, err = gh("", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner"); err != nil {
			return fail(fmt.Errorf("couldn't tell which GitHub repo this folder is, so give the pull request as OWNER/REPO#%d:\n%v", n, err))
		}
	}
	out, err := gh("", "pr", "view", strconv.Itoa(n), "-R", repo, "--json",
		"number,title,url,baseRefName,headRefName,isCrossRepository,headRepositoryOwner")
	if err != nil {
		return fail(fmt.Errorf("couldn't look up %s#%d:\n%v", repo, n, err))
	}
	var info pr
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		return fail(fmt.Errorf("gh pr view gave unexpected output: %w", err))
	}

	home := homeDir()
	cfgPath, err := config.Path()
	if err != nil {
		return fail(err)
	}
	var p plan
	if _, err := os.Stat(cfgPath); err != nil {
		p.noConfig = true
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fail(err)
	}
	if p.dir, err = reviewDir(repo, n); err != nil {
		return fail(err)
	}
	p.cfg, p.base = cfg.For(p.dir, home), cfg.Extends
	p.cfg.Hooks.Allow = nil // no host hooks in a review; project hooks never run
	p.cfg.MCP = nil         // nor MCP servers: the PR's text could steer what Claude does with them
	p.name = sandbox.Name(p.dir)
	p.rm = !keep
	p.command = "paddock review"
	if err := p.fill(home); err != nil {
		return fail(err)
	}
	if p.exists {
		return fail(fmt.Errorf("there's already a review sandbox for %s#%d, kept from an earlier --keep. Rejoin it with: paddock %s, or remove it with: paddock rm %s",
			repo, n, tilde(p.dir), tilde(p.dir)))
	}
	p.claudeArgs = claudeArgs
	if len(claudeArgs) == 0 {
		p.prompt = reviewPrompt(info)
		p.claudeArgs = []string{"--", p.prompt}
	}
	what := fmt.Sprintf("%s#%d %q", repo, n, info.Title)
	p.review = what + "  (deleted when you exit)"
	if keep {
		p.review = what + "  (kept after you exit)"
	}
	if dryRun {
		return show(p, verbose)
	}

	if keep {
		fmt.Printf("Reviewing %s in a throwaway sandbox. No hooks run; it is kept after you exit (paddock rm %s to delete it).\n", what, tilde(p.dir))
	} else {
		fmt.Printf("Reviewing %s in a throwaway sandbox. No hooks run; it is deleted when you exit.\n", what)
	}
	if err := cloneForReview(p.dir, repo, info); err != nil {
		os.RemoveAll(p.dir)
		return fail(err)
	}
	if err := writeKit(p.kitDir, kitOptions(p.hostDir, p.cfg)); err != nil {
		os.RemoveAll(p.dir)
		return fail(err)
	}
	// Ctrl-C belongs to Claude; paddock stays to clean up after it.
	signal.Notify(make(chan os.Signal, 1), os.Interrupt)
	code, err := sbx.Run(p.sbxArgs()...)
	if keep {
		fmt.Printf("The review sandbox and its clone at %s are kept. Rejoin with: paddock %s\n", tilde(p.dir), tilde(p.dir))
	} else {
		os.RemoveAll(p.dir)
		fmt.Println("Review sandbox removed.")
	}
	return exit(code, err)
}
