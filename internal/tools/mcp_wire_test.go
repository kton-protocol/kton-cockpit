package tools

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A real production failure (agent-host logs, 2026-07-23): cockpit_publish and cockpit_ask
// error paths return their Out struct's zero value via errResult, which for map/slice fields is
// nil — and encoding/json marshals a nil map/slice as JSON null, not {}/[] . The MCP Go SDK infers
// each tool's output schema from the struct via reflection and marks every field without
// `omitempty`/`omitzero` as required; validating a null value against a required object/array
// field fails with exactly: `type: <invalid reflect.Value> has type "null", want "object"`. This
// masked the tool's actual, useful error message behind a confusing schema-validation failure.
// Fixed by adding `omitempty` to every map/slice field in PublishOutput and AskOutput.
//
// Confirmed directly against the schema library (TestOutputSchema_PublishOutputZeroValueValidates
// below): with the omitempty tags removed, it reproduces the exact production error text
// (`type: <invalid reflect.Value> has type "null", want "object"`) for PublishOutput's zero
// value. TestOutputSchema_AskOutputZeroValueValidates passed even without the fix — an
// unexplained asymmetry in the validator between map- and slice-typed fields here, not further
// investigated — but omitempty is the correct, safe choice for both regardless. A wire-level
// attempt via this file's in-memory MCP client/server did not reproduce either failure, likely due
// to some difference between AddTool's cached/resolved schema and a freshly resolved one; the
// schema tests above, not the wire tests below, are what actually guard against a regression here.

func TestOutputSchema_PublishOutputZeroValueValidates(t *testing.T) {
	assertZeroValueValidatesAgainstOwnSchema(t, PublishOutput{})
}

func TestOutputSchema_AskOutputZeroValueValidates(t *testing.T) {
	assertZeroValueValidatesAgainstOwnSchema(t, AskOutput{})
}

func assertZeroValueValidatesAgainstOwnSchema[T any](t *testing.T, zero T) {
	t.Helper()
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		t.Fatalf("inferring schema: %v", err)
	}
	resolved, err := schema.Resolve(&jsonschema.ResolveOptions{ValidateDefaults: true})
	if err != nil {
		t.Fatalf("resolving schema: %v", err)
	}

	b, err := json.Marshal(zero)
	if err != nil {
		t.Fatalf("marshaling zero value: %v", err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("unmarshaling for validation: %v", err)
	}

	if err := resolved.Validate(v); err != nil {
		t.Fatalf("zero value %s failed its own output schema validation (this is the exact bug "+
			"that reached production via errResult's zero-value error path): %v", string(b), err)
	}
}

// These tests drive the tools through an actual in-memory MCP client/server pair — real
// marshaling — rather than calling the Go functions directly (as the other smoke tests do).
func newTestServerAndClient(t *testing.T) *mcp.ClientSession {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "cockpit_publish"}, Publish)
	mcp.AddTool(server, &mcp.Tool{Name: "cockpit_say"}, Say)
	mcp.AddTool(server, &mcp.Tool{Name: "cockpit_ask"}, Ask)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	t1, t2 := mcp.NewInMemoryTransports()

	ctx := context.Background()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestMCPWire_PublishErrorPathReturnsToolError(t *testing.T) {
	chdir(t, os.TempDir()) // outside any git repo — guaranteed to hit the config-load error path
	cs := newTestServerAndClient(t)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "cockpit_publish",
		Arguments: map[string]any{"outputs": []string{"x"}, "cmd": "echo hi"},
	})
	if err != nil {
		t.Fatalf("CallTool returned a protocol-level error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected a tool-level error outside any git repo")
	}
}

func TestMCPWire_AskErrorPathReturnsToolError(t *testing.T) {
	chdir(t, os.TempDir())
	cs := newTestServerAndClient(t)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "cockpit_ask",
		Arguments: map[string]any{"query": "producer", "ref": "sha256:deadbeef"},
	})
	if err != nil {
		t.Fatalf("CallTool returned a protocol-level error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected a tool-level error outside any git repo")
	}
}

func TestMCPWire_AskSuccessPathAgainstRealRegistry(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	cs := newTestServerAndClient(t)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "cockpit_ask",
		Arguments: map[string]any{"query": "producer", "ref": unknownHash},
	})
	if err != nil {
		t.Fatalf("CallTool returned a protocol-level error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got tool error: %+v", result.Content)
	}
}
