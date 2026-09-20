// Package mcp tests the local stdio MCP adapter over the Session Protocol.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
)

type executorFunc func(context.Context, protocol.Request) protocol.Response

func (f executorFunc) Execute(ctx context.Context, request protocol.Request) protocol.Response {
	return f(ctx, request)
}

func TestServerInitializesListsAndCallsSafeTools(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","clientInfo":{"name":"test","version":"1"},"capabilities":{}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_session_metrics","arguments":{"session_id":"s1"}}}`,
	}, "\n") + "\n"
	executor := executorFunc(func(_ context.Context, request protocol.Request) protocol.Response {
		if request.Operation != protocol.SessionMetrics {
			t.Fatalf("operation=%s", request.Operation)
		}
		return protocol.Response{Version: protocol.Version, RequestID: request.RequestID, Status: protocol.StatusOK, Data: map[string]any{"turn_count": 2}}
	})
	var output bytes.Buffer
	if err := New(executor).Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	responses := decodeLines(t, output.String())
	if len(responses) != 3 {
		t.Fatalf("responses=%d output=%s", len(responses), output.String())
	}
	tools := responses[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 10 {
		t.Fatalf("tools=%d", len(tools))
	}
	result := responses[2]["result"].(map[string]any)
	if result["isError"] != nil {
		t.Fatalf("unexpected error result: %v", result)
	}
	if result["structuredContent"].(map[string]any)["turn_count"] != float64(2) {
		t.Fatalf("result=%v", result)
	}
}

func TestServerRejectsUnknownMethodsToolsAndFailedOperations(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":"a","method":"unknown"}`,
		`{"jsonrpc":"2.0","id":"b","method":"tools/call","params":{"name":"shell_exec","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":"c","method":"tools/call","params":{"name":"get_session","arguments":{"session_id":"missing"}}}`,
	}, "\n") + "\n"
	executor := executorFunc(func(_ context.Context, request protocol.Request) protocol.Response {
		return protocol.Response{Version: protocol.Version, RequestID: request.RequestID, Status: protocol.StatusNotFound, Error: &protocol.Error{Code: "not_found", Message: "session not found"}}
	})
	var output bytes.Buffer
	if err := New(executor).Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	responses := decodeLines(t, output.String())
	if responses[0]["error"].(map[string]any)["code"] != float64(-32601) || responses[1]["error"].(map[string]any)["code"] != float64(-32602) {
		t.Fatalf("errors=%v", responses)
	}
	if responses[2]["result"].(map[string]any)["isError"] != true {
		t.Fatalf("failed operation=%v", responses[2])
	}
}

func TestServerNegotiatesLegacyAndRejectsMalformedInput(t *testing.T) {
	var output bytes.Buffer
	server := New(executorFunc(func(context.Context, protocol.Request) protocol.Response { return protocol.Response{} }))
	if err := server.Serve(context.Background(), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2026-07-28"}}`+"\n"+`not-json`+"\n"), &output); err != nil {
		t.Fatal(err)
	}
	responses := decodeLines(t, output.String())
	if responses[0]["result"].(map[string]any)["protocolVersion"] != LegacyProtocolVersion || responses[1]["error"].(map[string]any)["code"] != float64(-32700) {
		t.Fatalf("responses=%v", responses)
	}
}

func TestServeMessageSupportsRemoteMCPRequests(t *testing.T) {
	server := New(executorFunc(func(_ context.Context, request protocol.Request) protocol.Response {
		return protocol.Response{Version: 1, RequestID: request.RequestID, Status: protocol.StatusOK, Data: map[string]any{"ok": true}}
	}))
	body, err := server.ServeMessage(context.Background(), []byte(`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"list_sessions","arguments":{}}}`))
	if err != nil || !strings.Contains(string(body), `"structuredContent":{"ok":true}`) {
		t.Fatalf("body=%s err=%v", body, err)
	}
	body, err = server.ServeMessage(context.Background(), []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if err != nil || body != nil {
		t.Fatalf("notification body=%s err=%v", body, err)
	}
}

func TestServeMessageBoundsAndHelpers(t *testing.T) {
	server := New(executorFunc(func(context.Context, protocol.Request) protocol.Response { return protocol.Response{} }))
	if _, err := server.ServeMessage(context.Background(), bytes.Repeat([]byte("x"), maximumFrameBytes+1)); err == nil {
		t.Fatal("oversized frame accepted")
	}
	body, err := server.ServeMessage(context.Background(), []byte(`not-json`))
	if err != nil || !strings.Contains(string(body), `"code":-32700`) {
		t.Fatalf("body=%s err=%v", body, err)
	}
	if operation, ok := OperationForTool("list_sessions"); !ok || operation != protocol.SessionList {
		t.Fatalf("operation=%s ok=%v", operation, ok)
	}
	if _, ok := OperationForTool("shell_exec"); ok {
		t.Fatal("unsafe tool mapped")
	}
	if !strings.Contains(string(ErrorMessage(-32000, "limited", map[string]int{"retry_after": 1})), `"retry_after":1`) {
		t.Fatal("error data missing")
	}
}

func TestScopedServerHidesAndRefusesUngrantableTools(t *testing.T) {
	server := NewScoped(executorFunc(func(_ context.Context, request protocol.Request) protocol.Response {
		return protocol.Response{Version: 1, RequestID: request.RequestID, Status: protocol.StatusOK}
	}), func(operation protocol.Operation) bool { return operation == protocol.SessionList })
	listed, _ := server.ServeMessage(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if !strings.Contains(string(listed), `"list_sessions"`) || strings.Contains(string(listed), `"get_session_metrics"`) {
		t.Fatalf("list=%s", listed)
	}
	denied, _ := server.ServeMessage(context.Background(), []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_session_metrics","arguments":{"session_id":"s1"}}}`))
	if !strings.Contains(string(denied), `"isError":true`) {
		t.Fatalf("denied=%s", denied)
	}
}

func decodeLines(t *testing.T, body string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(body), "\n")
	values := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var value map[string]any
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	return values
}
