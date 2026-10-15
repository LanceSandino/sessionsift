// Package mcp verifies Desktop-compatible schemas without changing text results.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestToolOutputSchemasUseObjectRootForDesktopClients(t *testing.T) {
	for _, definition := range tools {
		if definition.OutputSchema["type"] != "object" {
			t.Errorf("%s outputSchema lacks required object root: %v", definition.Name, definition.OutputSchema)
		}
	}
}

func TestObjectResultPreservesObjectsAndWrapsScalarNullAndArray(t *testing.T) {
	for _, test := range []struct {
		data any
		want string
	}{
		{map[string]any{"ok": true}, `{"ok":true}`},
		{struct {
			OK bool `json:"ok"`
		}{true}, `{"ok":true}`},
		{[]string{"synthetic"}, `{"items":["synthetic"]}`},
		{nil, `{"value":null}`},
		{"synthetic", `{"value":"synthetic"}`},
	} {
		encoded, err := json.Marshal(test.data)
		if err != nil {
			t.Fatal(err)
		}
		result, err := json.Marshal(objectResult(test.data, encoded))
		if err != nil || string(result) != test.want {
			t.Fatalf("result=%s want=%s err=%v", result, test.want, err)
		}
	}
}

func TestSDKArrayResultsUseObjectEnvelopeAndPreserveText(t *testing.T) {
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	server := NewSDKServer(executorFunc(func(_ context.Context, request protocol.Request) protocol.Response {
		return protocol.Response{Version: 1, Status: protocol.StatusOK, RequestID: request.RequestID, Data: []string{"synthetic-only"}}
	}), nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Run(ctx, serverTransport) }()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "schema-regression", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "get_findings", Arguments: map[string]any{"session_id": "synthetic"}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil || string(encoded) != `{"items":["synthetic-only"]}` {
		t.Fatalf("structured=%s err=%v", encoded, err)
	}
	if len(result.Content) != 1 || result.Content[0].(*mcpsdk.TextContent).Text != `["synthetic-only"]` {
		t.Fatalf("text changed: %v", result.Content)
	}
}

func TestLegacyArrayResultsUseObjectEnvelopeAndPreserveText(t *testing.T) {
	server := New(executorFunc(func(_ context.Context, request protocol.Request) protocol.Response {
		return protocol.Response{Version: 1, Status: protocol.StatusOK, RequestID: request.RequestID, Data: []string{"synthetic-only"}}
	}))
	var output bytes.Buffer
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_findings","arguments":{"session_id":"synthetic"}}}` + "\n"
	if err := server.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	result := decodeLines(t, output.String())[0]["result"].(map[string]any)
	encoded, _ := json.Marshal(result["structuredContent"])
	if string(encoded) != `{"items":["synthetic-only"]}` {
		t.Fatalf("structured=%s", encoded)
	}
	if result["content"].([]any)[0].(map[string]any)["text"] != `["synthetic-only"]` {
		t.Fatalf("text changed: %v", result)
	}
}
