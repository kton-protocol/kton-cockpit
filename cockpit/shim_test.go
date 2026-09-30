package cockpit

import (
	"context"

	"github.com/kton-protocol/kton-cockpit/internal/mcpsurface"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The tests in this package predate the API and drive the verbs in the MCP handler shape — a result
// flagged IsError on refusal — which is also exactly what the tool surface exposes. These reach the
// API through the same adapter the MCP server registers, so they exercise both at once.

func Publish(ctx context.Context, req *mcp.CallToolRequest, in PublishRequest) (*mcp.CallToolResult, PublishResult, error) {
	return mcpsurface.Handler(New(Start{}).Publish)(ctx, req, in)
}

func Say(ctx context.Context, req *mcp.CallToolRequest, in SayRequest) (*mcp.CallToolResult, SayResult, error) {
	return mcpsurface.Handler(New(Start{}).Say)(ctx, req, in)
}

func Ask(ctx context.Context, req *mcp.CallToolRequest, in AskRequest) (*mcp.CallToolResult, AskResult, error) {
	return mcpsurface.Handler(New(Start{}).Ask)(ctx, req, in)
}
