package main

import (
	"context"
	"log"
	"net"
	"os"

	auth_pb "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	"google.golang.org/grpc"

	"github.com/agentgate/agentgate/internal/approval"
	"github.com/agentgate/agentgate/internal/audit"
	"github.com/agentgate/agentgate/internal/authz"
	"github.com/agentgate/agentgate/internal/policy"
	"github.com/agentgate/agentgate/internal/slack"
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

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://agentgate:agentgate@postgres:5432/agentgate"
	}
	auditLog, err := audit.Connect(context.Background(), dsn)
	if err != nil {
		log.Fatalf("audit log: %v", err)
	}
	log.Printf("audit log connected")

	// Approval flow: pending store + executor (TOCTOU re-check + replay),
	// human-facing HTTP server, optional Slack notifications.
	slackClient := &slack.Client{
		BotToken:      os.Getenv("SLACK_BOT_TOKEN"),
		Channel:       os.Getenv("SLACK_CHANNEL"),
		SigningSecret: os.Getenv("SLACK_SIGNING_SECRET"),
	}
	if slackClient.Enabled() {
		log.Printf("slack notifications enabled (channel %s)", slackClient.Channel)
	} else {
		log.Printf("slack not configured — approvals via local UI only")
	}
	toyMCPURL := os.Getenv("TOY_MCP_URL")
	if toyMCPURL == "" {
		toyMCPURL = "http://toy-mcp-server:8080/mcp"
	}
	approvalStore := approval.NewStore(auditLog.Pool())
	executor := approval.NewExecutor(approvalStore, engine, auditLog, toyMCPURL)
	approvalAddr := os.Getenv("APPROVAL_HTTP_ADDR")
	if approvalAddr == "" {
		approvalAddr = ":8090"
	}
	go func() {
		if err := approval.NewHTTPServer(approvalStore, executor, slackClient).ListenAndServe(approvalAddr); err != nil {
			log.Fatalf("approval http server: %v", err)
		}
	}()
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}

	grpcServer := grpc.NewServer()
	auth_pb.RegisterAuthorizationServer(grpcServer, authz.New(engine, auditLog, approvalStore, slackClient))

	log.Printf("agentgate ext_authz service listening on %s", addr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatal(err)
	}
}
