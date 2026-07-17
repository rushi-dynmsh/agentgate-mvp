package approval

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/agentgate/agentgate/internal/audit"
	"github.com/agentgate/agentgate/internal/identity"
	"github.com/agentgate/agentgate/internal/policy"
)

// Executor completes an approved call: it re-runs the policy check against
// CURRENT state (the human may approve minutes later — permissions can have
// changed in between: the classic time-of-check-to-time-of-use gap), and
// only on a passing re-check does it forward the original call to the tool.
type Executor struct {
	store  *Store
	engine *policy.Engine
	audit  *audit.Log
	// toyMCPURL is where approved calls are replayed (the backend the
	// gateway would have forwarded to).
	toyMCPURL string
}

func NewExecutor(store *Store, engine *policy.Engine, auditLog *audit.Log, toyMCPURL string) *Executor {
	return &Executor{store: store, engine: engine, audit: auditLog, toyMCPURL: toyMCPURL}
}

// Approve handles a human clicking "Approve". Returns a human-readable
// outcome for whoever clicked.
func (e *Executor) Approve(ctx context.Context, txID, decidedBy string) string {
	ok, err := e.store.Decide(ctx, txID, "approved", decidedBy)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	if !ok {
		return "already decided (or unknown transaction)"
	}
	p, err := e.store.Get(ctx, txID)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}

	// ── TOCTOU re-check ────────────────────────────────────────────────
	// Reload the policy file and re-evaluate with the identity captured at
	// park time. A permission revoked while the call sat parked means this
	// approval is void — a fresh deny, logged as such.
	if err := e.engine.Reload(); err != nil {
		log.Printf("approval: policy reload failed, refusing to execute: %v", err)
		return e.refuse(ctx, p, decidedBy, "policy reload failed")
	}
	who := &identity.Identity{AgentID: p.AgentID, OnBehalfOf: p.OnBehalfOf, Roles: p.Roles}
	decision, reason := e.engine.Decide(ctx, who, p.Tool, p.Args)
	if decision == policy.Deny {
		log.Printf("approval: TOCTOU re-check DENIED tx=%s tool=%s (%s)", txID, p.Tool, reason)
		return e.refuse(ctx, p, decidedBy, "re-check denied: "+reason)
	}

	// ── Execute the original call against the backend tool ────────────
	result, err := e.callTool(ctx, p.Tool, p.Args)
	if err != nil {
		e.store.SetResult(ctx, txID, "failed", err.Error())
		e.auditRow(ctx, p, "execute_failed", err.Error())
		return fmt.Sprintf("approved but execution failed: %v", err)
	}
	e.store.SetResult(ctx, txID, "executed", result)
	e.auditRow(ctx, p, "executed_after_approval", fmt.Sprintf("approved by %s; re-check passed (%s)", decidedBy, reason))
	log.Printf("approval: tx=%s tool=%s executed after approval by %s", txID, p.Tool, decidedBy)
	return "approved and executed: " + result
}

// Deny handles a human clicking "Deny".
func (e *Executor) Deny(ctx context.Context, txID, decidedBy string) string {
	ok, err := e.store.Decide(ctx, txID, "denied", decidedBy)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	if !ok {
		return "already decided (or unknown transaction)"
	}
	p, _ := e.store.Get(ctx, txID)
	if p != nil {
		e.store.SetResult(ctx, txID, "denied", "denied by "+decidedBy)
		e.auditRow(ctx, p, "denied_by_human", "denied by "+decidedBy)
	}
	log.Printf("approval: tx=%s denied by %s", txID, decidedBy)
	return "denied"
}

// refuse marks an approved-but-refused call (failed TOCTOU re-check).
func (e *Executor) refuse(ctx context.Context, p *Pending, decidedBy, why string) string {
	e.store.SetResult(ctx, p.TransactionID, "failed", why)
	e.auditRow(ctx, p, "deny", "approval by "+decidedBy+" voided — "+why)
	return "approval voided: " + why
}

func (e *Executor) auditRow(ctx context.Context, p *Pending, decision, reason string) {
	if _, err := e.audit.Record(ctx, audit.Entry{
		AgentID:       p.AgentID,
		OnBehalfOf:    p.OnBehalfOf,
		Roles:         p.Roles,
		Method:        "approval/" + decision,
		Tool:          p.Tool,
		Args:          p.Args,
		Decision:      decision,
		Reason:        reason,
		PolicyVersion: e.engine.Version(),
	}); err != nil {
		log.Printf("AUDIT WRITE FAILED (approval %s): %v", decision, err)
	}
}

// callTool opens a short-lived MCP session straight to the backend tool
// server (bypassing the gateway — the decision to allow has already been
// made and audited) and invokes the parked call.
func (e *Executor) callTool(ctx context.Context, tool string, args map[string]any) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	c, err := mcpclient.NewStreamableHttpClient(e.toyMCPURL)
	if err != nil {
		return "", fmt.Errorf("mcp client: %w", err)
	}
	defer c.Close()
	if _, err := c.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo:      mcp.Implementation{Name: "agentgate-executor", Version: "0.1.0"},
		},
	}); err != nil {
		return "", fmt.Errorf("mcp initialize: %w", err)
	}
	res, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: tool, Arguments: args},
	})
	if err != nil {
		return "", fmt.Errorf("call tool: %w", err)
	}
	out, _ := json.Marshal(res.Content)
	if res.IsError {
		return "", fmt.Errorf("tool returned error: %s", out)
	}
	return string(out), nil
}
