package main

import (
	"log"
	"net"
	"os"

	auth_pb "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	"google.golang.org/grpc"

	"github.com/agentgate/agentgate/internal/authz"
	"github.com/agentgate/agentgate/internal/policy"
)

func main() {
	addr := ":9000"
	if v := os.Getenv("GRPC_ADDR"); v != "" {
		addr = v
	}

	policyFile := os.Getenv("POLICY_FILE")
	if policyFile == "" {
		policyFile = "/policies/agentgate.cedar"
	}
	engine, err := policy.New(policyFile)
	if err != nil {
		log.Fatalf("load policies: %v", err)
	}
	log.Printf("loaded cedar policies from %s (version %s)", policyFile, engine.Version())
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}

	grpcServer := grpc.NewServer()
	auth_pb.RegisterAuthorizationServer(grpcServer, authz.New(engine))

	log.Printf("agentgate ext_authz service listening on %s", addr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatal(err)
	}
}
