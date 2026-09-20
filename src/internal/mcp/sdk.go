package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/LanceSandino/sessionsift/src/internal/app"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const ModernProtocolVersion = "2026-07-28"

var sdkRequest atomic.Uint64

// NewSDKServer builds a current, backwards-compatible MCP server from the canonical Session Protocol.
func NewSDKServer(executor Executor, allowed func(protocol.Operation) bool) *mcpsdk.Server {
	return newSDKServer(executor, allowed, false)
}

func newSDKServer(executor Executor, allowed func(protocol.Operation) bool, oauth bool) *mcpsdk.Server {
	if allowed == nil {
		allowed = func(protocol.Operation) bool { return true }
	}
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "sessionsift", Version: app.Version()}, &mcpsdk.ServerOptions{
		Instructions: "Read-only access to redacted local SessionSift intelligence. Lists and timelines are bounded; follow returned continuation guidance before acting on every session.",
		Capabilities: &mcpsdk.ServerCapabilities{},
	})
	closed := false
	for _, definition := range tools {
		definition := definition
		if !allowed(definition.operation) {
			continue
		}
		tool := &mcpsdk.Tool{
			Name: definition.Name, Title: definition.Title, Description: definition.Description,
			InputSchema: definition.InputSchema, OutputSchema: definition.OutputSchema,
			Annotations: &mcpsdk.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: &closed, IdempotentHint: true, OpenWorldHint: &closed},
		}
		if oauth {
			// The current Go SDK models extension fields through _meta. OpenAI MCP
			// clients use this declaration together with protected-resource discovery.
			tool.Meta = mcpsdk.Meta{"securitySchemes": []map[string]any{{"type": "oauth2", "scopes": []string{"session.read"}}}}
		}
		server.AddTool(tool, func(ctx context.Context, request *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			if executor == nil {
				return nil, errors.New("MCP executor is required")
			}
			arguments := request.Params.Arguments
			if len(arguments) == 0 {
				arguments = json.RawMessage("{}")
			}
			response := executor.Execute(ctx, protocol.Request{
				Version: protocol.Version, RequestID: "mcp-" + strconv.FormatUint(sdkRequest.Add(1), 10),
				Operation: definition.operation, Arguments: arguments,
			})
			if response.Status != protocol.StatusOK {
				message := "operation failed"
				if response.Error != nil {
					message = response.Error.Message
				}
				return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: message}}, IsError: true}, nil
			}
			encoded, err := json.Marshal(response.Data)
			if err != nil {
				return nil, err
			}
			return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(encoded)}}, StructuredContent: response.Data}, nil
		})
	}
	return server
}

// ServeSDK runs the official MCP SDK over caller-provided stdio streams.
func ServeSDK(ctx context.Context, executor Executor, input io.Reader, output io.Writer) error {
	err := NewSDKServer(executor, nil).Run(ctx, &mcpsdk.IOTransport{Reader: io.NopCloser(input), Writer: nopWriteCloser{output}})
	if errors.Is(err, io.EOF) || (err != nil && strings.Contains(err.Error(), "closing: EOF")) {
		return nil
	}
	return err
}

// SDKHTTPHandler serves modern stateless Streamable HTTP and legacy stateless clients.
func SDKHTTPHandler(executor Executor, allowed func(protocol.Operation) bool) http.Handler {
	transport := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
		return newSDKServer(executor, allowed, true)
	}, &mcpsdk.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, MaxRequestBodyBytes: maximumFrameBytes,
		PropagateRequestCancellation: true, DisableLocalhostProtection: true,
	})
	return withTopLevelOAuthToolSecurity(transport)
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
