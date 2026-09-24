// Package api exposes owner-approved remote access and collaboration routes.
// @module RemoteAPI keeps pairing, grants, audit, and collaboration behind local authority.
package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/accessauth"
	"github.com/LanceSandino/sessionsift/src/internal/agent/service"
	mcpserver "github.com/LanceSandino/sessionsift/src/internal/mcp"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
	"github.com/LanceSandino/sessionsift/src/internal/remote"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

func remoteRoute(store *storage.Store, manager *remote.Manager, identity remote.Identity, verifier accessauth.Verifier, publicURL string, hosted *remote.HostedRuntime, w http.ResponseWriter, r *http.Request) {
	inviter := hosted.Inviter()
	if hostedRoute(store, hosted, w, r) {
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/remote/status":
		writeJSON(w, http.StatusOK, map[string]any{"device_id": identity.ID, "pending_pairings": manager.PendingPairings(), "connections": manager.Connections(), "oauth_grants": manager.OAuthGrants(), "invite_grants": manager.InviteGrants(), "oauth_configured": verifier != nil, "hosted_invites_configured": inviter != nil, "bridge": hosted.Status(), "audit": manager.AuditTail(500)})
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/remote/invitations":
		if inviter == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "hosted invitations are not configured"})
			return
		}
		values, err := inviter.List(r.Context())
		remoteWrite(w, http.StatusOK, values, err)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/remote/invitations":
		if inviter == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "hosted invitations are not configured"})
			return
		}
		var body struct {
			Invitee       string `json:"invitee"`
			SessionID     string `json:"session_id"`
			Role          string `json:"role"`
			DurationHours int    `json:"duration_hours"`
		}
		if !decodeRemote(w, r, &body) {
			return
		}
		if _, err := store.GetSession(r.Context(), body.SessionID); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}
		value, err := inviter.Create(r.Context(), body.Invitee, body.SessionID, body.Role, time.Duration(body.DurationHours)*time.Hour)
		remoteWrite(w, http.StatusCreated, value, err)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/remote/invitations/revoke":
		if inviter == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "hosted invitations are not configured"})
			return
		}
		var body struct {
			InvitationID string `json:"invitation_id"`
		}
		if !decodeRemote(w, r, &body) {
			return
		}
		err := inviter.Revoke(r.Context(), body.InvitationID)
		remoteWrite(w, http.StatusOK, map[string]bool{"revoked": err == nil}, err)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/remote/oauth/grants":
		var body struct {
			Principal    string               `json:"principal"`
			Operations   []protocol.Operation `json:"operations"`
			DurationDays int                  `json:"duration_days"`
		}
		if !decodeRemote(w, r, &body) {
			return
		}
		grant, err := manager.ApproveOAuthPrincipal(body.Principal, body.Operations, time.Duration(body.DurationDays)*24*time.Hour)
		remoteWrite(w, http.StatusCreated, grant, err)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/remote/pairings":
		pairing, err := manager.CreatePairing(identity.ID)
		remoteWrite(w, http.StatusCreated, pairing, err)
	case r.Method == http.MethodPost && (r.URL.Path == "/api/v1/remote/pairings/claim" || r.URL.Path == "/remote/v1/pairings/claim"):
		var body struct {
			Code       string               `json:"code"`
			Reviewer   string               `json:"reviewer"`
			Operations []protocol.Operation `json:"operations"`
		}
		if !decodeRemote(w, r, &body) {
			return
		}
		pairing, err := manager.ClaimPairingFrom(body.Code, body.Reviewer, body.Operations, remoteSource(r))
		remoteWrite(w, http.StatusOK, pairing, err)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/remote/pairings/approve":
		var body struct {
			Code            string `json:"code"`
			DurationMinutes int    `json:"duration_minutes"`
		}
		if !decodeRemote(w, r, &body) {
			return
		}
		grant, token, err := manager.ApprovePairing(body.Code, time.Duration(body.DurationMinutes)*time.Minute)
		remoteWrite(w, http.StatusOK, map[string]any{"grant": grant, "token": token}, err)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/remote/pairings/deny":
		var body struct {
			Code string `json:"code"`
		}
		if !decodeRemote(w, r, &body) {
			return
		}
		err := manager.DenyPairing(body.Code)
		remoteWrite(w, http.StatusOK, map[string]bool{"denied": err == nil}, err)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/remote/grants/revoke":
		var body struct {
			GrantID string `json:"grant_id"`
		}
		if !decodeRemote(w, r, &body) {
			return
		}
		err := manager.Revoke(body.GrantID)
		remoteWrite(w, http.StatusOK, map[string]bool{"revoked": err == nil}, err)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/remote/connections/disconnect":
		var body struct {
			ConnectionID string `json:"connection_id"`
		}
		if !decodeRemote(w, r, &body) {
			return
		}
		err := manager.Disconnect(body.ConnectionID)
		remoteWrite(w, http.StatusOK, map[string]bool{"disconnected": err == nil}, err)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/remote/annotations":
		writeJSON(w, http.StatusOK, manager.Annotations(r.URL.Query().Get("session_id")))
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/remote/annotations":
		var body struct {
			SessionID string `json:"session_id"`
			TurnID    string `json:"turn_id"`
			Author    string `json:"author"`
			Body      string `json:"body"`
		}
		if !decodeRemote(w, r, &body) {
			return
		}
		value, err := manager.AddAnnotation(body.SessionID, body.TurnID, body.Author, body.Body)
		remoteWrite(w, http.StatusCreated, value, err)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/remote/reviews":
		writeJSON(w, http.StatusOK, manager.Reviews(r.URL.Query().Get("session_id")))
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/remote/reviews":
		var body struct {
			SessionID string `json:"session_id"`
			Reviewer  string `json:"reviewer"`
			Status    string `json:"status"`
			Summary   string `json:"summary"`
		}
		if !decodeRemote(w, r, &body) {
			return
		}
		value, err := manager.AddReview(body.SessionID, body.Reviewer, body.Status, body.Summary)
		remoteWrite(w, http.StatusCreated, value, err)
	case r.Method == http.MethodPost && r.URL.Path == "/remote/v1/protocol":
		remoteProtocol(store, manager, w, r)
	case r.URL.Path == "/remote/v1/mcp":
		remoteMCP(store, manager, w, r)
	case r.URL.Path == "/mcp":
		oauthMCP(store, manager, verifier, publicURL, w, r)
	case strings.HasPrefix(r.URL.Path, "/remote/v1/annotations"):
		remoteCollaboration(manager, protocol.AnnotationList, protocol.AnnotationCreate, w, r)
	case strings.HasPrefix(r.URL.Path, "/remote/v1/reviews"):
		remoteCollaboration(manager, protocol.ReviewList, protocol.ReviewCreate, w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func oauthMCP(store *storage.Store, manager *remote.Manager, verifier accessauth.Verifier, publicURL string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !allowMCPOrigin(w, r) {
		mcpRefusal(w, http.StatusForbidden, -32600, "origin is not allowed", nil)
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if verifier == nil {
		mcpRefusal(w, http.StatusServiceUnavailable, -32000, "Cloudflare Access origin verification is not configured", nil)
		return
	}
	principal, err := verifier.Verify(r.Context(), r.Header.Get("Cf-Access-Jwt-Assertion"))
	if err != nil {
		w.Header().Set("WWW-Authenticate", oauthChallenge(publicURL, r))
		mcpRefusal(w, http.StatusUnauthorized, -32600, "OAuth authentication required", nil)
		return
	}
	grant, retryAfter, err := manager.AllowPrincipalRequest(principal.Email)
	if err != nil {
		if strings.Contains(err.Error(), "rate limit") {
			seconds := int(retryAfter.Seconds()) + 1
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			mcpRefusal(w, http.StatusTooManyRequests, -32000, "rate limit exceeded", map[string]int{"retry_after": seconds})
			return
		}
		mcpRefusal(w, http.StatusForbidden, -32001, "this OAuth identity has not been locally approved or its grant expired", nil)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil {
		mcpRefusal(w, http.StatusRequestEntityTooLarge, -32600, "MCP frame too large", nil)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	allowed := map[protocol.Operation]bool{}
	for _, operation := range grant.Operations {
		allowed[operation] = true
	}
	agent := service.New(store, remote.NewPrincipalAuthorizer(manager, principal.Email))
	capture := &auditWriter{ResponseWriter: w, status: http.StatusOK}
	mcpserver.SDKHTTPHandler(agent, func(operation protocol.Operation) bool { return allowed[operation] }).ServeHTTP(capture, r)
	manager.RecordAudit(remote.AuditRecord{ClientID: principal.Email, GrantID: grant.ID, Operation: mcpOperation(body), Allowed: capture.status < 400, ResponseBytes: capture.bytes, Status: "oauth_mcp"})
}

type auditWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (writer *auditWriter) WriteHeader(status int) {
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *auditWriter) Write(body []byte) (int, error) {
	written, err := writer.ResponseWriter.Write(body)
	writer.bytes += written
	return written, err
}

func protectedResourceMetadata(publicURL string, w http.ResponseWriter, r *http.Request) {
	resource := publicResourceURL(publicURL, r)
	issuer := strings.TrimRight(os.Getenv("SESSIONSIFT_ACCESS_ISSUER"), "/")
	servers := []string{}
	if issuer != "" {
		servers = append(servers, issuer)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"resource": resource, "authorization_servers": servers,
		"scopes_supported":       []string{"openid", "profile", "email", "offline_access", "session.read"},
		"resource_documentation": resource + "/docs/mcp",
	})
}

func publicResourceURL(configured string, r *http.Request) string {
	configured = strings.TrimRight(strings.TrimSpace(configured), "/")
	if configured != "" {
		return configured
	}
	scheme := "https"
	if r.TLS == nil && (strings.HasPrefix(r.Host, "127.0.0.1") || strings.HasPrefix(r.Host, "localhost")) {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}

func oauthChallenge(publicURL string, r *http.Request) string {
	return `Bearer resource_metadata="` + publicResourceURL(publicURL, r) + `/.well-known/oauth-protected-resource", scope="session.read"`
}

func remoteMCP(store *storage.Store, manager *remote.Manager, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Allow", "POST, OPTIONS")
	if !allowMCPOrigin(w, r) {
		mcpRefusal(w, http.StatusForbidden, -32600, "origin is not allowed", nil)
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		mcpRefusal(w, http.StatusMethodNotAllowed, -32600, "send one JSON-RPC message with POST", nil)
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" || token == r.Header.Get("Authorization") {
		w.Header().Set("WWW-Authenticate", `Bearer scope="session.read"`)
		mcpRefusal(w, http.StatusUnauthorized, -32600, "authentication required", nil)
		return
	}
	grant, retryAfter, authErr := manager.AllowRequest(token)
	if authErr != nil {
		if strings.Contains(authErr.Error(), "rate limit") {
			seconds := int(retryAfter.Seconds()) + 1
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			mcpRefusal(w, http.StatusTooManyRequests, -32000, "rate limit exceeded", map[string]int{"retry_after": seconds})
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token", scope="session.read"`)
		mcpRefusal(w, http.StatusUnauthorized, -32600, "invalid or expired grant", nil)
		return
	}
	if version := r.Header.Get("MCP-Protocol-Version"); version != "" && version != mcpserver.LegacyProtocolVersion {
		mcpRefusal(w, http.StatusBadRequest, -32600, "unsupported MCP protocol version", map[string]any{"supported": []string{mcpserver.LegacyProtocolVersion}})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil {
		mcpRefusal(w, http.StatusBadRequest, -32600, "MCP frame too large", nil)
		return
	}
	if strings.HasPrefix(strings.TrimSpace(string(body)), "[") {
		mcpRefusal(w, http.StatusBadRequest, -32600, "JSON-RPC batches are not supported", nil)
		return
	}
	allowed := map[protocol.Operation]bool{}
	for _, operation := range grant.Operations {
		allowed[operation] = true
	}
	server := mcpserver.NewScoped(service.New(store, remote.NewGrantAuthorizer(manager, token)), func(operation protocol.Operation) bool { return allowed[operation] })
	response, err := server.ServeMessage(r.Context(), body)
	if err != nil {
		mcpRefusal(w, http.StatusBadRequest, -32600, "invalid MCP request", nil)
		return
	}
	manager.RecordAudit(remote.AuditRecord{ClientID: grant.Reviewer, GrantID: grant.ID, Operation: mcpOperation(body), Allowed: response != nil && !strings.Contains(string(response), `"isError":true`), ResponseBytes: len(response), Status: "mcp"})
	if response == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(response)
}

func remoteProtocol(store *storage.Store, manager *remote.Manager, w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" || token == r.Header.Get("Authorization") {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "bearer grant required"})
		return
	}
	if _, _, err := manager.AllowRequest(token); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid, expired, or limited grant"})
		return
	}
	var request protocol.Request
	if !decodeRemote(w, r, &request) {
		return
	}
	agent := service.New(store, remote.NewGrantAuthorizer(manager, token))
	response := agent.Execute(r.Context(), request)
	encoded, _ := json.Marshal(response)
	grant, _ := manager.GrantForToken(token)
	manager.RecordAudit(remote.AuditRecord{ClientID: grant.Reviewer, GrantID: grant.ID, Operation: request.Operation, Allowed: response.Status != protocol.StatusDenied, ResponseBytes: len(encoded), Status: string(response.Status)})
	status := http.StatusOK
	if response.Status != protocol.StatusOK {
		status = statusFor(response.Status)
	}
	writeJSON(w, status, response)
}

func remoteCollaboration(manager *remote.Manager, listOperation, createOperation protocol.Operation, w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" || token == r.Header.Get("Authorization") {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "bearer grant required"})
		return
	}
	operation := listOperation
	if r.Method == http.MethodPost {
		operation = createOperation
	} else if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if err := manager.Authorize(r.Context(), token, operation); err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "capability denied"})
		return
	}
	if listOperation == protocol.AnnotationList {
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, manager.Annotations(r.URL.Query().Get("session_id")))
			return
		}
		var body struct {
			SessionID string `json:"session_id"`
			TurnID    string `json:"turn_id"`
			Author    string `json:"author"`
			Body      string `json:"body"`
		}
		if decodeRemote(w, r, &body) {
			value, err := manager.AddAnnotation(body.SessionID, body.TurnID, body.Author, body.Body)
			remoteWrite(w, http.StatusCreated, value, err)
		}
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, manager.Reviews(r.URL.Query().Get("session_id")))
		return
	}
	var body struct {
		SessionID string `json:"session_id"`
		Reviewer  string `json:"reviewer"`
		Status    string `json:"status"`
		Summary   string `json:"summary"`
	}
	if decodeRemote(w, r, &body) {
		value, err := manager.AddReview(body.SessionID, body.Reviewer, body.Status, body.Summary)
		remoteWrite(w, http.StatusCreated, value, err)
	}
}

func allowMCPOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	allowed := map[string]bool{"http://127.0.0.1:7777": true, "http://localhost:7777": true}
	for _, value := range strings.Split(os.Getenv("SESSIONSIFT_ALLOWED_ORIGINS"), ",") {
		if value = strings.TrimSpace(value); value != "" {
			allowed[value] = true
		}
	}
	if !allowed[origin] {
		return false
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, MCP-Protocol-Version")
	return true
}

func mcpRefusal(w http.ResponseWriter, status, code int, message string, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(mcpserver.ErrorMessage(code, message, data))
}

func mcpOperation(body []byte) protocol.Operation {
	var request struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if json.Unmarshal(body, &request) != nil {
		return "mcp.invalid"
	}
	if operation, ok := mcpserver.OperationForTool(request.Params.Name); ok {
		return operation
	}
	return protocol.Operation("mcp." + request.Method)
}

func remoteSource(r *http.Request) string {
	host := r.RemoteAddr
	if parsed, err := url.Parse("tcp://" + r.RemoteAddr); err == nil && parsed.Hostname() != "" {
		host = parsed.Hostname()
	}
	return host
}
func decodeRemote(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return false
	}
	return true
}
func remoteWrite(w http.ResponseWriter, status int, value any, err error) {
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, status, value)
}
