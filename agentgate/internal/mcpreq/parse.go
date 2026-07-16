// Package mcpreq extracts MCP-level meaning (JSON-RPC method, tool name,
// arguments) out of the raw HTTP body agentgateway forwards on ext_authz
// check requests.
package mcpreq

import "encoding/json"

// Call describes what the MCP client is actually asking for.
type Call struct {
	// JSON-RPC method, e.g. "initialize", "tools/list", "tools/call".
	Method string
	// Tool name and arguments — only set when Method == "tools/call".
	Tool string
	Args map[string]any
	// Raw body, kept for the audit log / replay.
	Raw []byte
}

type jsonRPCEnvelope struct {
	Method string `json:"method"`
	Params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"params"`
}

// Parse decodes an MCP JSON-RPC request body. A body that isn't valid
// JSON-RPC yields a Call with empty Method — callers decide policy for that.
func Parse(body []byte) Call {
	c := Call{Raw: body}
	var env jsonRPCEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return c
	}
	c.Method = env.Method
	if env.Method == "tools/call" {
		c.Tool = env.Params.Name
		c.Args = env.Params.Arguments
	}
	return c
}
