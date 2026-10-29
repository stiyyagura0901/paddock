package main

import (
	"testing"

	"github.com/stiyyagura0901/paddock/internal/sandbox"
	"github.com/stiyyagura0901/paddock/internal/sbx"
)

// ls, stop, rm and shell must find a project's main and named sandboxes,
// and leave everyone else's alone.
func TestMatchMainNamedAndForeign(t *testing.T) {
	const dir = "/work/api"
	list := []sbx.Sandbox{
		{Name: sandbox.Name(dir), Workspaces: []string{dir}},
		{Name: sandbox.NamedName(dir, "feat-a"), Workspaces: []string{dir}},
		{Name: sandbox.Name(dir) + "-Not_Valid", Workspaces: []string{dir}}, // not a name paddock makes
		{Name: sandbox.NamedName("/work/other", "feat-a"), Workspaces: []string{dir}},
		{Name: "claude-api", Workspaces: []string{dir}}, // made by sbx itself
		{Name: sandbox.Name(dir)},                       // no workspace
	}
	mine, others := match(list)
	if len(mine) != 2 || others != 4 {
		t.Fatalf("got %d of ours and %d others, want 2 and 4: %+v", len(mine), others, mine)
	}
	if mine[0].named != "" || mine[1].named != "feat-a" {
		t.Errorf("names = %q, %q; want \"\" and \"feat-a\"", mine[0].named, mine[1].named)
	}
}
