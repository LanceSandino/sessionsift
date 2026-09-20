package mcp

// This module adapts the official MCP Go SDK's extension-only tool metadata to
// the top-level securitySchemes field required by OpenAI's OAuth integration.

import (
	"bytes"
	"encoding/json"
	"net/http"
)

func withTopLevelOAuthToolSecurity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured := &bufferedHTTPResponse{header: make(http.Header)}
		next.ServeHTTP(captured, r)

		body := captured.body.Bytes()
		if rewritten, changed := mirrorOAuthToolSecuritySchemes(body); changed {
			body = rewritten
			captured.header.Del("Content-Length")
		}
		for name, values := range captured.header {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		status := captured.status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	})
}

func mirrorOAuthToolSecuritySchemes(body []byte) ([]byte, bool) {
	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) != nil {
		return body, false
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(envelope["result"], &result) != nil {
		return body, false
	}
	var tools []map[string]json.RawMessage
	if json.Unmarshal(result["tools"], &tools) != nil {
		return body, false
	}

	changed := false
	for _, tool := range tools {
		if len(tool["securitySchemes"]) != 0 {
			continue
		}
		var metadata map[string]json.RawMessage
		if json.Unmarshal(tool["_meta"], &metadata) != nil || len(metadata["securitySchemes"]) == 0 {
			continue
		}
		tool["securitySchemes"] = append(json.RawMessage(nil), metadata["securitySchemes"]...)
		changed = true
	}
	if !changed {
		return body, false
	}

	encodedTools, err := json.Marshal(tools)
	if err != nil {
		return body, false
	}
	result["tools"] = encodedTools
	encodedResult, err := json.Marshal(result)
	if err != nil {
		return body, false
	}
	envelope["result"] = encodedResult
	rewritten, err := json.Marshal(envelope)
	if err != nil {
		return body, false
	}
	return rewritten, true
}

type bufferedHTTPResponse struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (w *bufferedHTTPResponse) Header() http.Header { return w.header }

func (w *bufferedHTTPResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *bufferedHTTPResponse) Write(value []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(value)
}

func (w *bufferedHTTPResponse) Flush() { w.WriteHeader(http.StatusOK) }
