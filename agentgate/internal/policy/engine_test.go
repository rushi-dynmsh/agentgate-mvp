package policy

import (
	"context"
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

var ctx = context.Background()

func args(id string) map[string]any { return map[string]any{"id": id} }

func TestReaderCanReadOnly(t *testing.T) {
	e := testEngine(t)
	if d, why := e.Decide(ctx, ident("bob", "reader"), "read_record", args("1")); d != Allow {
		t.Errorf("reader read_record = %s (%s), want allow", d, why)
	}
	if d, why := e.Decide(ctx, ident("bob", "reader"), "delete_record", args("1")); d != Deny {
		t.Errorf("reader delete_record = %s (%s), want deny", d, why)
	}
}

func TestAdminCanDoBothButDeleteNeedsApproval(t *testing.T) {
	e := testEngine(t)
	if d, why := e.Decide(ctx, ident("alice", "admin"), "read_record", args("1")); d != Allow {
		t.Errorf("admin read_record = %s (%s), want allow", d, why)
	}
	if d, why := e.Decide(ctx, ident("alice", "admin"), "delete_record", args("1")); d != NeedsApproval {
		t.Errorf("admin delete_record = %s (%s), want needs_approval", d, why)
	}
}

func TestNoRolesDenied(t *testing.T) {
	e := testEngine(t)
	if d, why := e.Decide(ctx, ident("mallory"), "read_record", args("1")); d != Deny {
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

// fakeGraph stands in for SpiceDB: alice owns record 1, nothing else.
type fakeGraph struct{}

func (fakeGraph) CheckOwner(_ context.Context, user, recordID string) (bool, error) {
	return user == "alice" && recordID == "1", nil
}

func TestOwnershipGatesDestructiveTools(t *testing.T) {
	e := testEngine(t)
	e.Relations = fakeGraph{}

	// Owner + admin role → parked for approval, as before.
	if d, why := e.Decide(ctx, ident("alice", "admin"), "delete_record", args("1")); d != NeedsApproval {
		t.Errorf("owner delete = %s (%s), want needs_approval", d, why)
	}
	// Admin role but NOT the owner of record 2 → denied by the graph.
	if d, why := e.Decide(ctx, ident("alice", "admin"), "delete_record", args("2")); d != Deny {
		t.Errorf("non-owner delete = %s (%s), want deny", d, why)
	}
	// Reads are untouched by the relationship layer.
	if d, why := e.Decide(ctx, ident("alice", "admin"), "read_record", args("2")); d != Allow {
		t.Errorf("read with graph = %s (%s), want allow", d, why)
	}
	// Missing record id on a destructive tool → fail closed.
	if d, why := e.Decide(ctx, ident("alice", "admin"), "delete_record", nil); d != Deny {
		t.Errorf("no-id delete = %s (%s), want deny", d, why)
	}
}
