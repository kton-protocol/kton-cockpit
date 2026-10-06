// Package mcpsurface puts the cockpit's three verbs on MCP, and does nothing else.
//
// It is a transport: each tool is one cockpit method, reached unchanged (SPEC §6). A refusal and a
// failure both come back as a tool-level error carrying the message, which is what an MCP client
// shows a session; nothing here decides anything.
//
// Deliberately minimal until the API and the command line are settled (ADR-005): the tool schemas
// are still inferred from the request and result types, jsonschema tags included.
package mcpsurface

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Handler adapts one cockpit method to the MCP SDK's tool-handler shape.
func Handler[In, Out any](fn func(context.Context, In) (*Out, error)) func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		var zero Out
		out, err := fn(ctx, in)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, zero, nil
		}
		return &mcp.CallToolResult{}, *out, nil
	}
}
