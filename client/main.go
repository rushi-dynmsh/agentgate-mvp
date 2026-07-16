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
	"net/http"
	"net/url"
	"strings"
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
	user := flag.String("user", "", "Keycloak username — fetches a token via password grant")
	password := flag.String("password", "", "Keycloak password (default: <user>-password)")
	keycloak := flag.String("keycloak", "http://localhost:8081/realms/agentgate", "Keycloak realm URL")
	listOnly := flag.Bool("list", false, "only list tools")
	flag.Parse()

	// -user alice is shorthand for "log in to Keycloak as alice and use the
	// resulting JWT". This is the agent acquiring credentials to act on
	// behalf of a human.
	if *user != "" && *token == "" {
		pw := *password
		if pw == "" {
			pw = *user + "-password" // test-realm convention (alice-password, bob-password)
		}
		t, err := fetchToken(*keycloak, *user, pw)
		if err != nil {
			log.Fatalf("fetch token for %s: %v", *user, err)
		}
		*token = t
		fmt.Printf("logged in to Keycloak as %s\n", *user)
	}

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

// fetchToken performs an OAuth2 "resource owner password" grant against
// Keycloak: username+password in, signed JWT access token out.
func fetchToken(realmURL, username, password string) (string, error) {
	resp, err := http.PostForm(
		strings.TrimSuffix(realmURL, "/")+"/protocol/openid-connect/token",
		url.Values{
			"grant_type": {"password"},
			"client_id":  {"agent-client"},
			"username":   {username},
			"password":   {password},
		})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var body struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if body.AccessToken == "" {
		return "", fmt.Errorf("keycloak: %s (HTTP %d)", body.Error, resp.StatusCode)
	}
	return body.AccessToken, nil
}
