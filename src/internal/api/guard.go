// Package api serves the localhost JSON API, hook receiver, and embedded UI.
// @module LocalGuard binds the localhost API to loopback hosts and same-origin browsers.
package api

import (
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// localGuard is the browser-facing trust boundary for the Local Agent. Binding
// to 127.0.0.1 does not stop a web page from reaching the port: DNS rebinding
// arrives with a foreign Host header, and cross-site forms/fetches arrive with
// a foreign Origin. Both are refused before any route runs.
func localGuard(next http.Handler) http.Handler {
	tunnelHost := tunnelHostname()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		host, ok := allowedHost(r.Host, tunnelHost)
		if !ok {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "host not allowed"})
			return
		}
		if !safeMethod(r.Method) && !corsManagedRoute(r.URL.Path) {
			if !sameOriginBrowser(r, host) {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-site request denied"})
				return
			}
			if requiresJSON(r.URL.Path) && !jsonContent(r.Header.Get("Content-Type")) {
				writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "application/json required"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func safeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// corsManagedRoute marks MCP endpoints that authenticate every request and
// apply their own explicit Origin allowlist for hosted AI clients.
func corsManagedRoute(path string) bool {
	return path == "/mcp" || path == "/remote/v1/mcp"
}

func requiresJSON(path string) bool {
	return strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/internal/hooks/")
}

func jsonContent(value string) bool {
	media, _, err := mime.ParseMediaType(value)
	return err == nil && media == "application/json"
}

// allowedHost accepts loopback names on any port, plus the configured public
// hostname only when the explicit Docker tunnel mode is enabled.
func allowedHost(value, tunnelHost string) (string, bool) {
	host := value
	if parsed, _, err := net.SplitHostPort(value); err == nil {
		host = parsed
	}
	host = strings.ToLower(strings.Trim(host, "[]"))
	switch {
	case host == "localhost" || host == "127.0.0.1" || host == "::1":
		return host, true
	case tunnelHost != "" && host == tunnelHost:
		return host, true
	default:
		return "", false
	}
}

func tunnelHostname() string {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("SESSIONSIFT_ALLOW_DOCKER_TUNNEL")), "true") {
		return ""
	}
	parsed, err := url.Parse(strings.TrimSpace(os.Getenv("SESSIONSIFT_PUBLIC_URL")))
	if err != nil || parsed.Scheme != "https" {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

// sameOriginBrowser allows non-browser clients (no Origin, no fetch metadata)
// and browsers whose Origin is exactly this allowed host.
func sameOriginBrowser(r *http.Request, host string) bool {
	site := strings.ToLower(r.Header.Get("Sec-Fetch-Site"))
	if site == "cross-site" || site == "same-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	originHost, ok := allowedHost(parsed.Host, host)
	return ok && originHost == host && strings.EqualFold(parsed.Host, r.Host)
}
