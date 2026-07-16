// Package authz implements the Envoy ext_authz Authorization gRPC service
// that agentgateway calls for every request on the governed route.
package authz

import (
	"context"
	"log"

	auth_pb "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	rpc_status "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"

	"github.com/agentgate/agentgate/internal/mcpreq"
)

// Server is the ext_authz Check handler. Phase 1: always allow, log
// everything — the plumbing stays identical when real decisions arrive.
type Server struct {
	auth_pb.UnimplementedAuthorizationServer
}

func New() *Server { return &Server{} }

func (s *Server) Check(ctx context.Context, req *auth_pb.CheckRequest) (*auth_pb.CheckResponse, error) {
	httpReq := req.GetAttributes().GetRequest().GetHttp()
	call := mcpreq.Parse([]byte(httpReq.GetBody()))

	authHeader := httpReq.GetHeaders()["authorization"]
	log.Printf("check: method=%q tool=%q args=%v path=%s auth_present=%v",
		call.Method, call.Tool, call.Args, httpReq.GetPath(), authHeader != "")

	return allow(), nil
}

func allow() *auth_pb.CheckResponse {
	return &auth_pb.CheckResponse{
		Status: &rpc_status.Status{Code: int32(codes.OK)},
		HttpResponse: &auth_pb.CheckResponse_OkResponse{
			OkResponse: &auth_pb.OkHttpResponse{},
		},
	}
}
