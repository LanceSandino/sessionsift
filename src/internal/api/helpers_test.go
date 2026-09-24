// Package api provides shared fixtures for localhost API tests.
package api

import (
	"io"
	"net/http"
	"net/http/httptest"
)

// localRequest builds a request as the loopback UI would send it: a loopback
// Host header and a JSON body type. Tests of the guard override these fields.
func localRequest(method, target string, body io.Reader) *http.Request {
	request := httptest.NewRequest(method, target, body)
	request.Host = "127.0.0.1:7777"
	if body != nil && method != http.MethodGet && method != http.MethodHead && requiresJSON(request.URL.Path) {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}
