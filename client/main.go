// Test client: connects to agentgateway's MCP listener, lists tools, and
// invokes the tool given on the command line.
//
// Usage:
//   go run . -tool read_record -id 1
//   go run . -tool delete_record -id 2 -token <JWT>
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

func main() {
	url := flag.String("url", "http://localhost:3000/mcp", "gateway MCP endpoint")
	tool := flag.String("tool", "read_record", "tool to call")
	id := flag.String("id", "1", "record id argument")
	token := flag.String("token", "", "bearer token (Authorization header)")
	listOnly := flag.Bool("list", false, "only list tools")
	flag.Parse()

	var opts []transport.StreamableHTTPCOption
	if *token != "" {
		opts = append(opts, transport.WithHTTPHeaders(map[string]string{
			"Authorization": "Bearer " + *token,
		}))
	}
	c, err := client.NewStreamableHttpClient(*url, opts...)
	if err != nil {
		log.Fatalf("create client: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := c.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo:      mcp.Implementation{Name: "agentgate-test-client", Version: "0.1.0"},
		},
	}); err != nil {
		log.Fatalf("initialize: %v", err)
	}

	tools, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		log.Fatalf("list tools: %v", err)
	}
	fmt.Println("tools exposed by gateway:")
	for _, t := range tools.Tools {
		fmt.Printf("  - %s: %s\n", t.Name, t.Description)
	}
	if *listOnly {
		return
	}

	fmt.Printf("\ncalling %s(id=%s)...\n", *tool, *id)
	res, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      *tool,
			Arguments: map[string]any{"id": *id},
		},
	})
	if err != nil {
		log.Fatalf("call tool: %v", err)
	}
	out, _ := json.MarshalIndent(res, "", "  ")
	fmt.Printf("isError=%v\nresult:\n%s\n", res.IsError, out)
}
