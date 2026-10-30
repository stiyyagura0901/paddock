package main

import (
	"reflect"
	"testing"

	"github.com/stiyyagura0901/paddock/internal/hostcfg"
)

// Host servers must reach sbx exactly as Claude Code runs them, and ones
// that need secrets must not be copied over without them.
func TestSbxAddArgs(t *testing.T) {
	for _, tc := range []struct {
		s    hostcfg.MCPServer
		want []string
	}{
		{hostcfg.MCPServer{Type: "http", URL: "https://mcp.linear.app/mcp"}, []string{"--url", "https://mcp.linear.app/mcp"}},
		{hostcfg.MCPServer{Type: "stdio", Command: "circleci", Args: []string{"mcp", "start"}},
			[]string{"--command", "circleci", "--args", "mcp", "--args", "start"}},
		// sbx reads each --args as CSV.
		{hostcfg.MCPServer{Type: "stdio", Command: "x", Args: []string{"a,b", `say "hi"`, ""}},
			[]string{"--command", "x", "--args", `"a,b"`, "--args", `"say ""hi"""`, "--args", `""`}},
		{hostcfg.MCPServer{Type: "stdio", Command: "x", Env: []string{"TOKEN"}}, nil},
		{hostcfg.MCPServer{Type: "http", URL: "https://x", Headers: []string{"Authorization"}}, nil},
		{hostcfg.MCPServer{Type: "ws-ide", URL: "ws://x"}, nil},
	} {
		got, err := sbxAddArgs(tc.s)
		if tc.want == nil && err == nil {
			t.Errorf("%+v: expected an error, got %q", tc.s, got)
		}
		if tc.want != nil && !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%+v: got %q, %v; want %q", tc.s, got, err, tc.want)
		}
	}
}
