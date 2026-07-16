// Package authz implements the Envoy ext_authz Authorization gRPC service
// that agentgateway calls for every request on the governed route.
package authz

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	core_pb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	type_pb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	rpc_status "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"

	auth_pb "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"

	"github.com/agentgate/agentgate/internal/approval"
	"github.com/agentgate/agentgate/internal/audit"
	"github.com/agentgate/agentgate/internal/identity"
	"github.com/agentgate/agentgate/internal/mcpreq"
	"github.com/agentgate/agentgate/internal/policy"
	"github.com/agentgate/agentgate/internal/slack"
)

// Server is the ext_authz Check handler: the single choke point every MCP
// request passes through before it may reach a backend tool.
type Server struct {
	auth_pb.UnimplementedAuthorizationServer
	engine    *policy.Engine
	audit     *audit.Log
	approvals *approval.Store
	slack     *slack.Client
}

func New(engine *policy.Engine, auditLog *audit.Log, approvals *approval.Store, slackClient *slack.Client) *Server {
	return &Server{engine: engine, audit: auditLog, approvals: approvals, slack: slackClient}
}

// record writes the decision to the audit log before it is returned. An
// audit failure is loud in the logs but does not block traffic — for a
// hackathon POC availability wins; a production build would fail closed.
func (s *Server) record(ctx context.Context, who *identity.Identity, call mcpreq.Call, decision, reason string) string {
	e := audit.Entry{
		Method:        call.Method,
		Tool:          call.Tool,
		Args:          call.Args,
		Decision:      decision,
		Reason:        reason,
		PolicyVersion: s.engine.Version(),
	}
	if who != nil {
		e.AgentID, e.OnBehalfOf, e.Roles = who.AgentID, who.OnBehalfOf, who.Roles
	}
	txID, err := s.audit.Record(ctx, e)
	if err != nil {
		log.Printf("AUDIT WRITE FAILED (decision=%s tool=%s): %v", decision, call.Tool, err)
	}
	return txID
}

func (s *Server) Check(ctx context.Context, req *auth_pb.CheckRequest) (*auth_pb.CheckResponse, error) {
	httpReq := req.GetAttributes().GetRequest().GetHttp()
	call := mcpreq.Parse([]byte(httpReq.GetBody()))

	// Who is calling? The gateway already verified the JWT (strict mode),
	// so a resolution failure here means misconfiguration, not forgery —
	// but we still fail closed.
	who, err := identity.FromCheckRequest(req)
	if err != nil {
		log.Printf("check: method=%q tool=%q — identity resolution failed: %v",
			call.Method, call.Tool, err)
		return deny(call, map[string]string{
			"status":  "denied",
			"message": "identity could not be resolved",
		}), nil
	}

	// Only tool invocations are policy-checked. Protocol plumbing
	// (initialize, tools/list, notifications) is harmless and always
	// allowed for any authenticated caller.
	if call.Method != "tools/call" {
		log.Printf("check: method=%q agent=%q on_behalf_of=%q — allow (protocol message)",
			call.Method, who.AgentID, who.OnBehalfOf)
		s.record(ctx, who, call, "allow", "protocol message")
		return allow(), nil
	}

	decision, reason := s.engine.Decide(who, call.Tool)
	log.Printf("check: tool=%q args=%v agent=%q on_behalf_of=%q roles=%v → %s (%s) policy=%s",
		call.Tool, call.Args, who.AgentID, who.OnBehalfOf, who.Roles,
		decision, reason, s.engine.Version())
	txID := s.record(ctx, who, call, string(decision), reason)

	switch decision {
	case policy.Allow:
		return allow(), nil
	case policy.NeedsApproval:
		// Park the call: record it as pending, notify a human, and tell
		// the agent it's waiting. ext_authz has no "hold" state, so the
		// deny carries a structured pending_approval status the agent
		// (or the human driving it) can act on.
		if err := s.approvals.Park(ctx, approval.Pending{
			TransactionID: txID,
			AgentID:       who.AgentID,
			OnBehalfOf:    who.OnBehalfOf,
			Roles:         who.Roles,
			Tool:          call.Tool,
			Args:          call.Args,
		}); err != nil {
			log.Printf("check: parking tx=%s failed: %v — failing closed", txID, err)
			return deny(call, map[string]string{
				"status":  "error",
				"message": "could not park call for approval",
			}), nil
		}
		if s.slack.Enabled() {
			if err := s.slack.PostApprovalRequest(txID, call.Tool, who.OnBehalfOf, who.AgentID, call.Args); err != nil {
				log.Printf("check: slack notify failed for tx=%s: %v (local approval UI still works)", txID, err)
			}
		}
		log.Printf("check: tool=%q parked for approval (tx %s)", call.Tool, txID)
		return deny(call, map[string]string{
			"status":         "pending_approval",
			"message":        fmt.Sprintf("%s is destructive and requires human approval; transaction %s is pending", call.Tool, txID),
			"transaction_id": txID,
		}), nil
	default:
		return deny(call, map[string]string{
			"status":         "denied",
			"message":        fmt.Sprintf("policy denied %s for %s (roles %v)", call.Tool, who.OnBehalfOf, who.Roles),
			"transaction_id": txID,
		}), nil
	}
}

func allow() *auth_pb.CheckResponse {
	return &auth_pb.CheckResponse{
		Status: &rpc_status.Status{Code: int32(codes.OK)},
		HttpResponse: &auth_pb.CheckResponse_OkResponse{
			OkResponse: &auth_pb.OkHttpResponse{},
		},
	}
}

// deny builds a rejection the CALLING AGENT can actually read. ext_authz is
// binary allow/deny, so the trick is in how the deny surfaces: we synthesize
// a valid JSON-RPC error response (echoing the request id) and return it
// with HTTP 200 — every MCP client parses that and shows the agent a clean,
// structured reason instead of a raw transport error.
func deny(call mcpreq.Call, detail map[string]string) *auth_pb.CheckResponse {
	detailJSON, _ := json.Marshal(detail)

	// Notifications have no id and expect no response body.
	id := call.ID
	if id == nil {
		id = json.RawMessage("null")
	}
	rpcErr, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"error": map[string]any{
			"code":    -32003, // implementation-defined server error: request refused
			"message": detail["message"],
			"data":    json.RawMessage(detailJSON),
		},
	})
	return &auth_pb.CheckResponse{
		Status: &rpc_status.Status{Code: int32(codes.PermissionDenied), Message: detail["message"]},
		HttpResponse: &auth_pb.CheckResponse_DeniedResponse{
			DeniedResponse: &auth_pb.DeniedHttpResponse{
				Status: &type_pb.HttpStatus{Code: type_pb.StatusCode_OK},
				Body:   string(rpcErr),
				Headers: []*core_pb.HeaderValueOption{{
					Header: &core_pb.HeaderValue{Key: "content-type", Value: "application/json"},
				}},
			},
		},
	}
}
