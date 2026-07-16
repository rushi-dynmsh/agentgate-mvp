// toy-server is a minimal MCP (Model Context Protocol) server. It exists so
// AgentGate has something realistic to protect: one safe tool (read_record)
// and one destructive tool (delete_record).
//
// It speaks MCP's "streamable HTTP" transport — plain HTTP POSTs carrying
// JSON-RPC messages to the /mcp endpoint. Agents never talk to this server
// directly; they go through agentgateway, which enforces policy first.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// A deliberately tiny in-memory "database" so tool calls return believable data.
var records = map[string]string{
	"1": "Customer: Acme Corp — plan: enterprise — status: active",
	"2": "Customer: Globex Inc — plan: starter — status: trialing",
	"3": "Customer: Initech LLC — plan: pro — status: past_due",
}

func main() {
	s := server.NewMCPServer("toy-server", "0.1.0",
		server.WithToolCapabilities(false),
	)

	readTool := mcp.NewTool("read_record",
		mcp.WithDescription("Read a customer record by id (safe, read-only)."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Record id to read")),
	)
	s.AddTool(readTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		rec, ok := records[id]
		if !ok {
			return mcp.NewToolResultError(fmt.Sprintf("no record with id %q", id)), nil
		}
		log.Printf("read_record id=%s", id)
		return mcp.NewToolResultText(rec), nil
	})

	deleteTool := mcp.NewTool("delete_record",
		mcp.WithDescription("Delete a customer record by id (DESTRUCTIVE)."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Record id to delete")),
	)
	s.AddTool(deleteTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if _, ok := records[id]; !ok {
			return mcp.NewToolResultError(fmt.Sprintf("no record with id %q", id)), nil
		}
		delete(records, id)
		log.Printf("delete_record id=%s (DESTRUCTIVE)", id)
		return mcp.NewToolResultText(fmt.Sprintf("record %s deleted", id)), nil
	})

	addr := ":8080"
	if v := os.Getenv("PORT"); v != "" {
		addr = ":" + v
	}
	log.Printf("toy MCP server listening on %s (streamable HTTP at /mcp)", addr)
	httpServer := server.NewStreamableHTTPServer(s)
	if err := httpServer.Start(addr); err != nil {
		log.Fatal(err)
	}
}
