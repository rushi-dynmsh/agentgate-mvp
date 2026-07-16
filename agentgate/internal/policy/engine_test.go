package policy

import (
	"path/filepath"
	"testing"

	"github.com/agentgate/agentgate/internal/identity"
)

// The tests evaluate the real policy file the service ships with, so a bad
// policy edit fails CI before it ever reaches the gateway.
func testEngine(t *testing.T) *Engine {
	t.Helper()
	e, err := New(filepath.Join("..", "..", "..", "policies", "agentgate.cedar"))
	if err != nil {
		t.Fatalf("load policies: %v", err)
	}
	return e
}

func ident(user string, roles ...string) *identity.Identity {
	return &identity.Identity{AgentID: "agent-client", OnBehalfOf: user, Roles: roles}
}

func TestReaderCanReadOnly(t *testing.T) {
	e := testEngine(t)
	if d, why := e.Decide(ident("bob", "reader"), "read_record"); d != Allow {
		t.Errorf("reader read_record = %s (%s), want allow", d, why)
	}
	if d, why := e.Decide(ident("bob", "reader"), "delete_record"); d != Deny {
		t.Errorf("reader delete_record = %s (%s), want deny", d, why)
	}
}

func TestAdminCanDoBothButDeleteNeedsApproval(t *testing.T) {
	e := testEngine(t)
	if d, why := e.Decide(ident("alice", "admin"), "read_record"); d != Allow {
		t.Errorf("admin read_record = %s (%s), want allow", d, why)
	}
	if d, why := e.Decide(ident("alice", "admin"), "delete_record"); d != NeedsApproval {
		t.Errorf("admin delete_record = %s (%s), want needs_approval", d, why)
	}
}

func TestNoRolesDenied(t *testing.T) {
	e := testEngine(t)
	if d, why := e.Decide(ident("mallory"), "read_record"); d != Deny {
		t.Errorf("roleless read_record = %s (%s), want deny", d, why)
	}
}

func TestDestructiveClassification(t *testing.T) {
	for tool, want := range map[string]bool{
		"delete_record":  true,
		"transfer_funds": true,
		"read_record":    false,
		"list_records":   false,
	} {
		if got := IsDestructive(tool); got != want {
			t.Errorf("IsDestructive(%s) = %v, want %v", tool, got, want)
		}
	}
}
