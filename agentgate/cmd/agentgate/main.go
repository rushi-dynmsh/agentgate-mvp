package main

import (
	"log"
	"net"
	"os"

	auth_pb "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	"google.golang.org/grpc"

	"github.com/agentgate/agentgate/internal/authz"
)

func main() {
	addr := ":9000"
	if v := os.Getenv("GRPC_ADDR"); v != "" {
		addr = v
	}
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}

	grpcServer := grpc.NewServer()
	auth_pb.RegisterAuthorizationServer(grpcServer, authz.New())

	log.Printf("agentgate ext_authz service listening on %s", addr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatal(err)
	}
}
