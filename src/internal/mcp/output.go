// Package mcp keeps structured output object-shaped for strict MCP clients.
package mcp

// objectResult preserves existing text payloads and object fields. MCP requires
// an object root for structuredContent; array results therefore use an items
// envelope while unusual scalar/null executor results use a value envelope.
// The precomputed JSON comes from the same successful marshal as text content.
func objectResult(data any, encoded []byte) any {
	if len(encoded) > 0 {
		switch encoded[0] {
		case '{':
			return data
		case '[':
			return map[string]any{"items": data}
		}
	}
	return map[string]any{"value": data}
}
