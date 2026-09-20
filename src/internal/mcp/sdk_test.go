package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSDKHTTPNegotiatesModernProtocolAndCallsTool(t *testing.T) {
	executor := executorFunc(func(_ context.Context, request protocol.Request) protocol.Response {
		return protocol.Response{Version: protocol.Version, RequestID: request.RequestID, Status: protocol.StatusOK, Data: map[string]any{"operation": request.Operation}}
	})
	handler := SDKHTTPHandler(executor, func(operation protocol.Operation) bool { return operation == protocol.SessionList })
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Result(), nil
	})}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "sessionsift-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcpsdk.StreamableClientTransport{Endpoint: "https://sessionsift.test/mcp", HTTPClient: httpClient, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if got := session.InitializeResult().ProtocolVersion; got != ModernProtocolVersion {
		t.Fatalf("protocol=%s", got)
	}
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil || len(listed.Tools) != 1 || listed.Tools[0].Name != "list_sessions" {
		t.Fatalf("tools=%v err=%v", listed, err)
	}
	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "list_sessions", Arguments: map[string]any{}})
	if err != nil || result.IsError || result.StructuredContent.(map[string]any)["operation"] != "session.list" {
		t.Fatalf("result=%v err=%v", result, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestServeSDKUsesOfficialStdioTransport(t *testing.T) {
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	server := NewSDKServer(executorFunc(func(_ context.Context, request protocol.Request) protocol.Response {
		return protocol.Response{Version: 1, RequestID: request.RequestID, Status: protocol.StatusOK, Data: map[string]any{"ok": true}}
	}), nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Run(ctx, serverTransport) }()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listed, err := session.ListTools(ctx, nil)
	if err != nil || len(listed.Tools) < 7 {
		t.Fatalf("tools=%v err=%v", listed, err)
	}
}
