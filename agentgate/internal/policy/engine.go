// Package policy answers "may this identity call this tool?" using Cedar,
// AWS's open-source authorization language. Policies live in a plain-text
// file (policies/agentgate.cedar) that is version-controlled — changing who
// can do what is a policy-file edit, never a code change.
package policy

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"sync"

	cedar "github.com/cedar-policy/cedar-go"

	"github.com/agentgate/agentgate/internal/identity"
)

// Decision is the tri-state outcome AgentGate works with. Cedar itself only
// answers allow/deny; NeedsApproval is our layer on top for destructive
// tools (see Decide).
type Decision string

const (
	Allow         Decision = "allow"
	Deny          Decision = "deny"
	NeedsApproval Decision = "needs_approval"
)

// Engine evaluates Cedar policies. Safe for concurrent use; Reload swaps the
// policy set atomically.
type Engine struct {
	mu       sync.RWMutex
	policies *cedar.PolicySet
	path     string
	// Version identifies which policy text produced a decision (for the
	// audit log). Currently the file's SHA-256 — see Reload.
	version string
}

// destructivePrefixes classifies tools by naming convention: anything that
// destroys or moves data out needs a human in the loop.
var destructivePrefixes = []string{"delete_", "transfer_", "drop_", "remove_"}

// IsDestructive reports whether a tool is classified destructive.
func IsDestructive(tool string) bool {
	for _, p := range destructivePrefixes {
		if strings.HasPrefix(tool, p) {
			return true
		}
	}
	return false
}

// New loads the Cedar policy file and returns a ready engine.
func New(path string) (*Engine, error) {
	e := &Engine{path: path}
	if err := e.Reload(); err != nil {
		return nil, err
	}
	return e, nil
}

// Reload re-reads the policy file from disk (used at startup and by the
// TOCTOU re-check to pick up policy changes made while a call was parked).
func (e *Engine) Reload() error {
	data, err := os.ReadFile(e.path)
	if err != nil {
		return fmt.Errorf("read policy file: %w", err)
	}
	ps, err := cedar.NewPolicySetFromBytes(e.path, data)
	if err != nil {
		return fmt.Errorf("parse cedar policies: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.policies = ps
	e.version = fmt.Sprintf("%x", sha256Sum(data))[:12]
	return nil
}

// Version returns a short identifier of the currently loaded policy text.
func (e *Engine) Version() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.version
}

// Decide runs the Cedar check for (who, tool) and layers the
// needs-approval rule on top:
//
//	Cedar deny                        → Deny
//	Cedar allow + read-only tool      → Allow
//	Cedar allow + destructive tool    → NeedsApproval (park for a human)
func (e *Engine) Decide(who *identity.Identity, tool string) (Decision, string) {
	e.mu.RLock()
	ps := e.policies
	e.mu.RUnlock()

	principal := cedar.NewEntityUID("AgentGate::User", cedar.String(who.OnBehalfOf))
	action := cedar.NewEntityUID("AgentGate::Action", "call_tool")
	resource := cedar.NewEntityUID("AgentGate::Tool", cedar.String(tool))

	risk := "read"
	if IsDestructive(tool) {
		risk = "destructive"
	}

	// Build the entity graph Cedar evaluates against: the user belongs to
	// their role groups; the tool carries its risk attribute.
	roleUIDs := []cedar.EntityUID{}
	for _, r := range who.Roles {
		roleUIDs = append(roleUIDs, cedar.NewEntityUID("AgentGate::Role", cedar.String(r)))
	}
	entities := cedar.EntityMap{
		principal: cedar.Entity{
			UID:     principal,
			Parents: cedar.NewEntityUIDSet(roleUIDs...),
		},
		resource: cedar.Entity{
			UID:        resource,
			Attributes: cedar.NewRecord(cedar.RecordMap{"risk": cedar.String(risk)}),
		},
	}
	for _, uid := range roleUIDs {
		entities[uid] = cedar.Entity{UID: uid}
	}

	ok, diag := ps.IsAuthorized(entities, cedar.Request{
		Principal: principal,
		Action:    action,
		Resource:  resource,
		Context:   cedar.NewRecord(cedar.RecordMap{}),
	})

	reason := fmt.Sprintf("cedar=%v risk=%s roles=%v", ok, risk, who.Roles)
	if len(diag.Errors) > 0 {
		reason += fmt.Sprintf(" errors=%v", diag.Errors)
	}

	if ok != cedar.Allow {
		return Deny, reason
	}
	if risk == "destructive" {
		return NeedsApproval, reason
	}
	return Allow, reason
}

func sha256Sum(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}
