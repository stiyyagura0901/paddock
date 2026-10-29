package doctor

import (
	"strings"
	"testing"
)

// A secret scoped to one sandbox doesn't help new sandboxes.
func TestGlobalSecret(t *testing.T) {
	out := `SCOPE                        TYPE      NAME        SECRET
claude-api-123abc            service   github      (stored)
(global)                     service   anthropic   (oauth configured)
`
	if globalSecret(strings.NewReader(out), "github") {
		t.Error("sandbox-scoped github secret counted as global")
	}
	if !globalSecret(strings.NewReader(out), "anthropic") {
		t.Error("global anthropic secret not found")
	}
}
