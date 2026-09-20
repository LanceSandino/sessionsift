package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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
	var toolsListResponse []byte
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		response := recorder.Result()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return nil, err
		}
		_ = response.Body.Close()
		if bytes.Contains(body, []byte(`"tools"`)) {
			toolsListResponse = append([]byte(nil), body...)
		}
		response.Body = io.NopCloser(bytes.NewReader(body))
		return response, nil
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
	assertTopLevelOAuthSecurityScheme(t, toolsListResponse)
	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "list_sessions", Arguments: map[string]any{}})
	if err != nil || result.IsError || result.StructuredContent.(map[string]any)["operation"] != "session.list" {
		t.Fatalf("result=%v err=%v", result, err)
	}
}

func assertTopLevelOAuthSecurityScheme(t *testing.T, body []byte) {
	t.Helper()
	var envelope struct {
		Result struct {
			Tools []map[string]json.RawMessage `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode tools/list response: %v body=%s", err, body)
	}
	if len(envelope.Result.Tools) != 1 {
		t.Fatalf("tools/list response has %d tools: %s", len(envelope.Result.Tools), body)
	}
	var schemes []struct {
		Type   string   `json:"type"`
		Scopes []string `json:"scopes"`
	}
	if err := json.Unmarshal(envelope.Result.Tools[0]["securitySchemes"], &schemes); err != nil {
		t.Fatalf("decode top-level securitySchemes: %v body=%s", err, body)
	}
	if len(schemes) != 1 || schemes[0].Type != "oauth2" || len(schemes[0].Scopes) != 1 || schemes[0].Scopes[0] != "session.read" {
		t.Fatalf("securitySchemes=%v", schemes)
	}
}

func TestMirrorOAuthToolSecuritySchemesPreservesUnrelatedResponses(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed JSON", body: `not-json`},
		{name: "tool call result", body: `{"jsonrpc":"2.0","id":3,"result":{"content":[]}}`},
		{name: "explicit top-level policy", body: `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"list_sessions","securitySchemes":[{"type":"noauth"}],"_meta":{"securitySchemes":[{"type":"oauth2","scopes":["session.read"]}]}}]}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rewritten, changed := mirrorOAuthToolSecuritySchemes([]byte(test.body))
			if changed || string(rewritten) != test.body {
				t.Fatalf("changed=%v response=%s", changed, rewritten)
			}
		})
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
