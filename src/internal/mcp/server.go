// Package mcp exposes SessionSift's allowlisted Local Agent operations over MCP stdio.
// @module MCPServer maps seven read-only tools to the canonical Session Protocol.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync/atomic"

	"github.com/LanceSandino/sessionsift/src/internal/app"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
)

const (
	LegacyProtocolVersion = "2025-11-25"
	maximumFrameBytes     = 4 << 20
)

type Executor interface {
	Execute(context.Context, protocol.Request) protocol.Response
}

type Server struct {
	executor Executor
	request  atomic.Uint64
	allowed  func(protocol.Operation) bool
}

func New(executor Executor) *Server {
	return &Server{executor: executor, allowed: func(protocol.Operation) bool { return true }}
}

func NewScoped(executor Executor, allowed func(protocol.Operation) bool) *Server {
	server := New(executor)
	if allowed != nil {
		server.allowed = allowed
	}
	return server
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type tool struct {
	Name         string         `json:"name"`
	Title        string         `json:"title"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema"`
	Annotations  map[string]any `json:"annotations"`
	operation    protocol.Operation
}

var tools = []tool{
	toolDefinition("list_sessions", "List sessions", "List redacted local Codex and ChatGPT session summaries. Results are paged; follow next_cursor until has_more is false before acting on every session.", protocol.SessionList, properties(map[string]any{"include_internal": map[string]any{"type": "boolean", "description": "Include internal review sessions."}, "cursor": map[string]any{"type": "integer", "minimum": 0}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}})),
	toolDefinition("get_session", "Get session", "Get one redacted session with turns, events, tool calls, and findings.", protocol.SessionGet, requiredSession()),
	toolDefinition("get_session_metrics", "Get session metrics", "Calculate token, cache, timing, and tool metrics for one session.", protocol.SessionMetrics, requiredSession()),
	toolDefinition("get_session_timeline", "Get session timeline", "Get a bounded redacted timeline for one session.", protocol.SessionTimeline, propertiesRequired(map[string]any{"session_id": stringProperty("Session identifier."), "limit": map[string]any{"type": "integer", "minimum": 0, "maximum": 1000}}, "session_id")),
	toolDefinition("get_turn", "Get turn", "Get one redacted turn from a session.", protocol.TurnGet, propertiesRequired(map[string]any{"session_id": stringProperty("Session identifier."), "turn_id": stringProperty("Turn identifier.")}, "session_id", "turn_id")),
	toolDefinition("get_findings", "Get findings", "Get deterministic evidence-backed findings for one session.", protocol.SessionFindings, requiredSession()),
	toolDefinition("compare_sessions", "Compare sessions", "Compare observable metrics and findings for two sessions.", protocol.SessionCompare, propertiesRequired(map[string]any{"a": stringProperty("First session identifier."), "b": stringProperty("Second session identifier.")}, "a", "b")),
}

func toolDefinition(name, title, description string, operation protocol.Operation, schema map[string]any) tool {
	return tool{Name: name, Title: title, Description: description, InputSchema: schema, OutputSchema: map[string]any{"oneOf": []map[string]any{{"type": "object"}, {"type": "array"}}}, Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}, operation: operation}
}

func requiredSession() map[string]any {
	return propertiesRequired(map[string]any{"session_id": stringProperty("Session identifier.")}, "session_id")
}

func properties(values map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": values, "additionalProperties": false}
}

func propertiesRequired(values map[string]any, required ...string) map[string]any {
	schema := properties(values)
	schema["required"] = required
	return schema
}

func stringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "description": description}
}

func (s *Server) Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	if s.executor == nil {
		return errors.New("MCP executor is required")
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64<<10), maximumFrameBytes)
	encoder := json.NewEncoder(output)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Bytes()
		var request rpcRequest
		if err := json.Unmarshal(line, &request); err != nil {
			if err := encoder.Encode(errorResponse(nil, -32700, "parse error")); err != nil {
				return err
			}
			continue
		}
		response, reply := s.handle(ctx, request)
		if reply {
			if err := encoder.Encode(response); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

// ServeMessage handles one JSON-RPC request for Streamable HTTP adapters.
func (s *Server) ServeMessage(ctx context.Context, body []byte) ([]byte, error) {
	if len(body) > maximumFrameBytes {
		return nil, errors.New("MCP frame too large")
	}
	var request rpcRequest
	if err := json.Unmarshal(body, &request); err != nil {
		response, _ := json.Marshal(errorResponse(nil, -32700, "parse error"))
		return response, nil
	}
	response, reply := s.handle(ctx, request)
	if !reply {
		return nil, nil
	}
	return json.Marshal(response)
}

func (s *Server) handle(ctx context.Context, request rpcRequest) (rpcResponse, bool) {
	if request.JSONRPC != "2.0" || request.Method == "" {
		return errorResponse(request.ID, -32600, "invalid request"), true
	}
	if len(request.ID) == 0 {
		return rpcResponse{}, false
	}
	switch request.Method {
	case "initialize":
		return success(request.ID, map[string]any{"protocolVersion": negotiatedVersion(request.Params), "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}}, "serverInfo": map[string]any{"name": "sessionsift", "version": app.Version()}, "instructions": "Read-only access to redacted local session intelligence. Lists and timelines are bounded; follow returned continuation guidance before acting on every session."}), true
	case "ping":
		return success(request.ID, map[string]any{}), true
	case "resources/list":
		return success(request.ID, map[string]any{"resources": []any{}}), true
	case "prompts/list":
		return success(request.ID, map[string]any{"prompts": []any{}}), true
	case "tools/list":
		listed := make([]tool, 0, len(tools))
		for _, definition := range tools {
			if s.allowed(definition.operation) {
				listed = append(listed, definition)
			}
		}
		return success(request.ID, map[string]any{"tools": listed}), true
	case "tools/call":
		return s.call(ctx, request), true
	default:
		return errorResponse(request.ID, -32601, "method not found"), true
	}
}

func negotiatedVersion(params json.RawMessage) string {
	var value struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if json.Unmarshal(params, &value) == nil && value.ProtocolVersion == LegacyProtocolVersion {
		return value.ProtocolVersion
	}
	return LegacyProtocolVersion
}

// OperationForTool returns the canonical Session Protocol operation for an MCP tool.
func OperationForTool(name string) (protocol.Operation, bool) {
	definition, ok := toolByName(name)
	return definition.operation, ok
}

func (s *Server) call(ctx context.Context, request rpcRequest) rpcResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(request.Params, &params); err != nil || params.Name == "" {
		return errorResponse(request.ID, -32602, "invalid tool parameters")
	}
	definition, ok := toolByName(params.Name)
	if !ok {
		return errorResponse(request.ID, -32602, "unknown tool")
	}
	if !s.allowed(definition.operation) {
		return success(request.ID, map[string]any{"content": []map[string]string{{"type": "text", "text": "capability denied"}}, "isError": true})
	}
	if len(params.Arguments) == 0 {
		params.Arguments = []byte("{}")
	}
	protocolRequest := protocol.Request{Version: protocol.Version, RequestID: "mcp-" + strconv.FormatUint(s.request.Add(1), 10), Operation: definition.operation, Arguments: params.Arguments}
	response := s.executor.Execute(ctx, protocolRequest)
	if response.Status != protocol.StatusOK {
		message := "operation failed"
		if response.Error != nil {
			message = response.Error.Message
		}
		return success(request.ID, map[string]any{"content": []map[string]string{{"type": "text", "text": message}}, "isError": true})
	}
	encoded, err := json.Marshal(response.Data)
	if err != nil {
		return success(request.ID, map[string]any{"content": []map[string]string{{"type": "text", "text": fmt.Sprintf("encoding result: %v", err)}}, "isError": true})
	}
	return success(request.ID, map[string]any{"content": []map[string]string{{"type": "text", "text": string(encoded)}}, "structuredContent": response.Data})
}

func toolByName(name string) (tool, bool) {
	for _, value := range tools {
		if value.Name == name {
			return value, true
		}
	}
	return tool{}, false
}

func success(id json.RawMessage, result any) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func errorResponse(id json.RawMessage, code int, message string) rpcResponse {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}
}

// ErrorMessage creates a JSON-RPC refusal for an HTTP transport adapter.
func ErrorMessage(code int, message string, data any) []byte {
	response := errorResponse(nil, code, message)
	response.Error.Data = data
	body, _ := json.Marshal(response)
	return body
}
