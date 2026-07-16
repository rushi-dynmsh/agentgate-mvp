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

	"github.com/agentgate/agentgate/internal/identity"
	"github.com/agentgate/agentgate/internal/mcpreq"
	"github.com/agentgate/agentgate/internal/policy"
)

// Server is the ext_authz Check handler: the single choke point every MCP
// request passes through before it may reach a backend tool.
type Server struct {
	auth_pb.UnimplementedAuthorizationServer
	engine *policy.Engine
}

func New(engine *policy.Engine) *Server {
	return &Server{engine: engine}
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
		return allow(), nil
	}

	decision, reason := s.engine.Decide(who, call.Tool)
	log.Printf("check: tool=%q args=%v agent=%q on_behalf_of=%q roles=%v → %s (%s) policy=%s",
		call.Tool, call.Args, who.AgentID, who.OnBehalfOf, who.Roles,
		decision, reason, s.engine.Version())

	switch decision {
	case policy.Allow:
		return allow(), nil
	case policy.NeedsApproval:
		// Phase 3: the marker exists but parking arrives in Phase 5 —
		// for now an authorized-but-destructive call passes through.
		log.Printf("check: tool=%q would be parked for approval (Phase 5)", call.Tool)
		return allow(), nil
	default:
		return deny(call, map[string]string{
			"status":  "denied",
			"message": fmt.Sprintf("policy denied %s for %s (roles %v)", call.Tool, who.OnBehalfOf, who.Roles),
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
