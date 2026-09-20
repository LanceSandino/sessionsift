package bridge

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/accessauth"
	mcpserver "github.com/LanceSandino/sessionsift/src/internal/mcp"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
)

const bridgeRequestLimit = 10 << 20
const invitationCookiePrefix = "__Host-sessionsift_invite_"

type Handler struct {
	store     *Store
	verifier  accessauth.Verifier
	publicURL string
	logger    *slog.Logger
	mu        sync.Mutex
	attempts  map[string]attemptWindow
}

type attemptWindow struct {
	started time.Time
	count   int
}

func NewHandler(store *Store, verifier accessauth.Verifier, publicURL string) http.Handler {
	return newHandler(store, verifier, publicURL, slog.Default())
}

func newHandler(store *Store, verifier accessauth.Verifier, publicURL string, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		store: store, verifier: verifier, publicURL: strings.TrimRight(publicURL, "/"), logger: logger,
		attempts: map[string]attemptWindow{},
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	securityHeaders(w)
	switch {
	case r.Method == http.MethodGet && (strings.HasPrefix(r.URL.Path, "/assets/") || strings.HasPrefix(r.URL.Path, "/invite/assets/")):
		serveAsset(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/healthz":
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case r.Method == http.MethodGet && r.URL.Path == "/readyz":
		if err := h.store.Health(r.Context()); err != nil {
			writeError(w, http.StatusServiceUnavailable, "bridge storage is unavailable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	case r.Method == http.MethodGet && r.URL.Path == "/.well-known/oauth-protected-resource":
		h.protectedResource(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/docs/mcp":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, mcpDocumentation)
	case r.Method == http.MethodPost && r.URL.Path == "/agent/v1/enroll":
		h.enroll(w, r)
	case strings.HasPrefix(r.URL.Path, "/agent/v1/"):
		h.deviceRoute(w, r)
	case r.URL.Path == "/mcp":
		h.loggedMCP(w, r)
	case r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/api/v1/") || strings.HasPrefix(r.URL.Path, "/invite/"):
		principal, ok := h.principal(w, r)
		if !ok {
			return
		}
		h.authenticatedRoute(w, r, principal)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

// authenticatedRoute is the single entry point for every human or MCP route
// that can initiate access to private session data. Device relay routes use
// their independent Ed25519 boundary above and never pass through this gate.
func (h *Handler) authenticatedRoute(w http.ResponseWriter, r *http.Request, principal accessauth.Principal) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, hostedPage(connectPage))
	case strings.HasPrefix(r.URL.Path, "/api/v1/"):
		h.browserAPI(w, r, principal)
	case strings.HasPrefix(r.URL.Path, "/invite/"):
		h.invitationRoute(w, r, principal)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *Handler) principal(w http.ResponseWriter, r *http.Request) (accessauth.Principal, bool) {
	if h.verifier == nil {
		writeError(w, http.StatusServiceUnavailable, "Cloudflare Access origin verification is not configured")
		return accessauth.Principal{}, false
	}
	principal, err := h.verifier.Verify(r.Context(), r.Header.Get("Cf-Access-Jwt-Assertion"))
	if scoped, ok := h.verifier.(*accessauth.JWTVerifier); ok && err == nil && !strings.HasPrefix(r.URL.Path, "/invite/") && !scoped.IsPrimaryAudience(principal) {
		err = errors.New("invitation audience cannot authorize account or MCP routes")
	}
	if err != nil {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+h.resourceURL(r)+`/.well-known/oauth-protected-resource"`)
		writeError(w, http.StatusUnauthorized, "authentication required")
		return accessauth.Principal{}, false
	}
	email, err := normalizeEmail(principal.Email)
	if err != nil || strings.TrimSpace(principal.Subject) == "" {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+h.resourceURL(r)+`/.well-known/oauth-protected-resource"`)
		writeError(w, http.StatusUnauthorized, "authentication required")
		return accessauth.Principal{}, false
	}
	principal.Email = email
	return principal, true
}

func (h *Handler) browserAPI(w http.ResponseWriter, r *http.Request, principal accessauth.Principal) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOriginRequest(r, h.resourceURL(r)) {
		writeError(w, http.StatusForbidden, "same-origin request required")
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/me":
		writeJSON(w, http.StatusOK, principal)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/devices":
		devices, err := h.store.OwnedDevices(principal.Email)
		writeResult(w, http.StatusOK, devices, err)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/mcp-devices":
		devices, err := h.store.DevicesFor(principal.Email)
		writeResult(w, http.StatusOK, devices, err)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/workspaces":
		workspaces, err := h.store.WorkspacesFor(principal.Email)
		writeResult(w, http.StatusOK, workspaces, err)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/workspaces":
		var body struct {
			Name string `json:"name"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		workspace, err := h.store.CreateWorkspace(principal.Email, body.Name)
		writeResult(w, http.StatusCreated, workspace, err)
	case strings.HasPrefix(r.URL.Path, "/api/v1/workspaces/") && strings.HasSuffix(r.URL.Path, "/members"):
		h.workspaceMembers(w, r, principal)
	case r.Method == http.MethodDelete && strings.Contains(strings.TrimPrefix(r.URL.Path, "/api/v1/workspaces/"), "/members/"):
		h.removeWorkspaceMember(w, r, principal)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/mcp-target":
		target, err := h.store.ResolveMCPTarget(principal.Email)
		writeResult(w, http.StatusOK, target, err)
	case r.Method == http.MethodPut && r.URL.Path == "/api/v1/mcp-target":
		var target MCPTarget
		if !decodeJSON(w, r, &target) {
			return
		}
		err := h.store.SetMCPTarget(principal.Email, target.WorkspaceID, target.DeviceID)
		writeResult(w, http.StatusOK, target, err)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/enrollments":
		var body struct {
			WorkspaceID string `json:"workspace_id,omitempty"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		var value Enrollment
		var err error
		if strings.TrimSpace(body.WorkspaceID) == "" {
			value, err = h.store.CreateEnrollment(principal.Email)
		} else {
			value, err = h.store.CreateEnrollmentForWorkspace(principal.Email, body.WorkspaceID)
		}
		writeResult(w, http.StatusCreated, value, err)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/devices/"):
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/devices/")
		err := h.store.RevokeDevice(id, principal.Email)
		writeResult(w, http.StatusOK, map[string]bool{"revoked": err == nil}, err)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/protocol":
		var body struct {
			WorkspaceID string           `json:"workspace_id,omitempty"`
			DeviceID    string           `json:"device_id"`
			Request     protocol.Request `json:"request"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		if body.DeviceID == "" {
			target, err := h.store.ResolveMCPTarget(principal.Email)
			if err != nil {
				writeError(w, http.StatusForbidden, err.Error())
				return
			}
			body.DeviceID = target.DeviceID
			body.WorkspaceID = target.WorkspaceID
		} else if body.WorkspaceID != "" {
			if err := h.store.SetMCPTarget(principal.Email, body.WorkspaceID, body.DeviceID); err != nil {
				writeError(w, http.StatusForbidden, err.Error())
				return
			}
		}
		response := relayExecutor{store: h.store, principal: principal.Email, deviceID: body.DeviceID}.Execute(r.Context(), body.Request)
		writeJSON(w, http.StatusOK, response)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func workspaceMemberPath(path string) (string, string, bool) {
	remainder := strings.TrimPrefix(path, "/api/v1/workspaces/")
	parts := strings.Split(remainder, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] != "members" {
		return "", "", false
	}
	if len(parts) == 2 {
		return parts[0], "", true
	}
	if len(parts) == 3 && parts[2] != "" {
		return parts[0], parts[2], true
	}
	return "", "", false
}

func (h *Handler) workspaceMembers(w http.ResponseWriter, r *http.Request, principal accessauth.Principal) {
	workspaceID, _, ok := workspaceMemberPath(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		members, err := h.store.MembersFor(principal.Email, workspaceID)
		writeResult(w, http.StatusOK, members, err)
	case http.MethodPost:
		var body struct {
			Principal string `json:"principal"`
			Role      string `json:"role"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		member, err := h.store.AddMembership(principal.Email, workspaceID, body.Principal, body.Role)
		writeResult(w, http.StatusCreated, member, err)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) removeWorkspaceMember(w http.ResponseWriter, r *http.Request, principal accessauth.Principal) {
	workspaceID, member, ok := workspaceMemberPath(r.URL.Path)
	if !ok || member == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	err := h.store.RemoveMembership(principal.Email, workspaceID, member)
	writeResult(w, http.StatusOK, map[string]bool{"removed": err == nil}, err)
}

func (h *Handler) enroll(w http.ResponseWriter, r *http.Request) {
	if !h.allowEnrollment(remoteIP(r)) {
		w.Header().Set("Retry-After", "600")
		writeError(w, http.StatusTooManyRequests, "too many enrollment attempts")
		return
	}
	var body struct {
		Code      string `json:"code"`
		Name      string `json:"name"`
		PublicKey string `json:"public_key"`
		Proof     string `json:"proof"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	key, err := base64.RawURLEncoding.DecodeString(body.PublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		writeError(w, http.StatusBadRequest, "invalid device public key")
		return
	}
	proof, proofErr := base64.RawURLEncoding.DecodeString(body.Proof)
	message, _ := json.Marshal([]string{"sessionsift.enroll.v1", body.Code, body.Name, body.PublicKey})
	if proofErr != nil || !ed25519.Verify(ed25519.PublicKey(key), message, proof) {
		writeError(w, http.StatusUnauthorized, "device key possession proof required")
		return
	}
	device, err := h.store.Enroll(body.Code, body.Name, ed25519.PublicKey(key))
	writeResult(w, http.StatusCreated, device, err)
}

func (h *Handler) deviceRoute(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, bridgeRequestLimit))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "request is too large")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	deviceID, ok := h.authenticateDevice(w, r, body)
	if !ok {
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/agent/v1/tasks/next":
		task, found, nextErr := h.store.Next(deviceID)
		if nextErr != nil {
			writeError(w, http.StatusInternalServerError, "task queue is unavailable")
			return
		}
		if !found {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, task)
	case r.Method == http.MethodPost && r.URL.Path == "/agent/v1/tasks/complete":
		var value struct {
			TaskID    string `json:"task_id"`
			Response  string `json:"response"`
			Signature string `json:"signature"`
		}
		if !decodeBytes(w, body, &value) {
			return
		}
		response, responseErr := base64.RawURLEncoding.DecodeString(value.Response)
		signature, signatureErr := base64.RawURLEncoding.DecodeString(value.Signature)
		if responseErr != nil || signatureErr != nil {
			writeError(w, http.StatusBadRequest, "invalid completion encoding")
			return
		}
		err = h.store.Complete(deviceID, value.TaskID, response, signature)
		writeResult(w, http.StatusOK, map[string]bool{"completed": err == nil}, err)
	case r.Method == http.MethodPost && r.URL.Path == "/agent/v1/grants":
		var value struct {
			Principal  string               `json:"principal"`
			Operations []protocol.Operation `json:"operations"`
			ExpiresAt  time.Time            `json:"expires_at"`
		}
		if !decodeBytes(w, body, &value) {
			return
		}
		grant, grantErr := h.store.RegisterGrant(deviceID, value.Principal, value.Operations, value.ExpiresAt)
		writeResult(w, http.StatusCreated, grant, grantErr)
	case r.Method == http.MethodDelete && r.URL.Path == "/agent/v1/grants":
		var value struct {
			Principal string `json:"principal"`
		}
		if !decodeBytes(w, body, &value) {
			return
		}
		revokeErr := h.store.RevokeGrant(deviceID, value.Principal)
		writeResult(w, http.StatusOK, map[string]bool{"revoked": revokeErr == nil}, revokeErr)
	case r.Method == http.MethodPost && r.URL.Path == "/agent/v1/invitations":
		var value struct {
			Owner     string    `json:"owner"`
			Invitee   string    `json:"invitee"`
			SessionID string    `json:"session_id"`
			Role      string    `json:"role"`
			ExpiresAt time.Time `json:"expires_at"`
		}
		if !decodeBytes(w, body, &value) {
			return
		}
		invitation, code, inviteErr := h.store.CreateInvitation(deviceID, value.Owner, value.Invitee, value.SessionID, value.Role, value.ExpiresAt)
		if inviteErr != nil {
			writeResult(w, http.StatusCreated, nil, inviteErr)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"invitation": invitation,
			"accept_url": h.publicURL + "/invite/accept/" + code,
		})
	case r.Method == http.MethodGet && r.URL.Path == "/agent/v1/invitations":
		values, listErr := h.store.InvitationsForDevice(deviceID)
		writeResult(w, http.StatusOK, values, listErr)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/agent/v1/invitations/"):
		invitationID := strings.TrimPrefix(r.URL.Path, "/agent/v1/invitations/")
		revokeErr := h.store.RevokeInvitation(deviceID, invitationID)
		writeResult(w, http.StatusOK, map[string]bool{"revoked": revokeErr == nil}, revokeErr)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *Handler) invitationRoute(w http.ResponseWriter, r *http.Request, principal accessauth.Principal) {
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/invite/accept/") {
		code := strings.TrimPrefix(r.URL.Path, "/invite/accept/")
		invitation, token, err := h.store.AcceptInvitation(code, principal.Email)
		if err != nil {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		maxAge := int(time.Until(invitation.ExpiresAt).Seconds())
		if maxAge < 1 {
			writeError(w, http.StatusForbidden, "invitation has expired")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: invitationCookieName(invitation.ID), Value: token, Path: "/", MaxAge: maxAge, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		http.Redirect(w, r, "/invite/"+invitation.ID, http.StatusSeeOther)
		return
	}
	invitationID, action, ok := invitationPath(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	token, ok := invitationToken(r, invitationID)
	if !ok {
		writeError(w, http.StatusUnauthorized, "invitation session required")
		return
	}
	if action == "" {
		// Metadata authorization uses the stored session rather than the public ID.
		invitation, err := h.store.invitationSession(principal.Email, invitationID, token)
		if err != nil {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, hostedPage(invitePage))
		_ = invitation
		return
	}
	if action == "metadata" && r.Method == http.MethodGet {
		invitation, err := h.store.invitationSession(principal.Email, invitationID, token)
		writeResult(w, http.StatusOK, invitation, err)
		return
	}
	if action == "protocol" && r.Method == http.MethodPost {
		if !sameOriginRequest(r, h.resourceURL(r)) {
			writeError(w, http.StatusForbidden, "cross-site request denied")
			return
		}
		var request protocol.Request
		if !decodeJSON(w, r, &request) {
			return
		}
		response := invitationRelayExecutor{store: h.store, principal: principal.Email, invitationID: invitationID, token: token}.Execute(r.Context(), request)
		writeJSON(w, http.StatusOK, response)
		return
	}
	writeError(w, http.StatusNotFound, "not found")
}

func invitationPath(path string) (string, string, bool) {
	trimmed := strings.TrimPrefix(path, "/invite/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) == 1 && strings.HasPrefix(parts[0], "invite_") {
		return parts[0], "", true
	}
	if len(parts) == 2 && strings.HasPrefix(parts[0], "invite_") && (parts[1] == "metadata" || parts[1] == "protocol") {
		return parts[0], parts[1], true
	}
	return "", "", false
}

func invitationToken(r *http.Request, invitationID string) (string, bool) {
	cookie, err := r.Cookie(invitationCookieName(invitationID))
	if err != nil {
		return "", false
	}
	if cookie.Value == "" {
		return "", false
	}
	return cookie.Value, true
}

func invitationCookieName(invitationID string) string {
	return invitationCookiePrefix + strings.TrimPrefix(invitationID, "invite_")
}

func sameOriginRequest(r *http.Request, resourceURL string) bool {
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	origin := strings.TrimRight(r.Header.Get("Origin"), "/")
	return origin == "" || origin == strings.TrimRight(resourceURL, "/")
}

func (h *Handler) authenticateDevice(w http.ResponseWriter, r *http.Request, body []byte) (string, bool) {
	deviceID := r.Header.Get("X-SessionSift-Device")
	timestamp := r.Header.Get("X-SessionSift-Timestamp")
	parsed, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil || time.Since(parsed).Abs() > 5*time.Minute {
		writeError(w, http.StatusUnauthorized, "device authentication failed")
		return "", false
	}
	key, err := h.store.DeviceKey(deviceID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "device authentication failed")
		return "", false
	}
	signature, err := base64.RawURLEncoding.DecodeString(r.Header.Get("X-SessionSift-Signature"))
	if err != nil || !ed25519.Verify(key, deviceSignatureMessage(r.Method, r.URL.Path, timestamp, body), signature) {
		writeError(w, http.StatusUnauthorized, "device authentication failed")
		return "", false
	}
	if err = h.store.ConsumeSignature(deviceID, signature, parsed.Add(5*time.Minute)); err != nil {
		writeError(w, http.StatusUnauthorized, "device authentication failed")
		return "", false
	}
	return deviceID, true
}

func deviceSignatureMessage(method, path, timestamp string, body []byte) []byte {
	digest := sha256.Sum256(body)
	return []byte(method + "\n" + path + "\n" + timestamp + "\n" + hex.EncodeToString(digest[:]))
}

func (h *Handler) mcp(w http.ResponseWriter, r *http.Request, principal accessauth.Principal) {
	target, err := h.store.ResolveMCPTarget(principal.Email)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	allowed := make(map[protocol.Operation]bool)
	for _, operation := range protocol.ReadOperations() {
		allowed[operation] = true
	}
	executor := relayExecutor{store: h.store, principal: principal.Email, deviceID: target.DeviceID}
	mcpserver.SDKHTTPHandler(executor, func(operation protocol.Operation) bool { return allowed[operation] }).ServeHTTP(w, r)
}

type relayExecutor struct {
	store     *Store
	principal string
	deviceID  string
}

type invitationRelayExecutor struct {
	store        *Store
	principal    string
	invitationID string
	token        string
}

func (e invitationRelayExecutor) Execute(ctx context.Context, request protocol.Request) protocol.Response {
	task, err := e.store.SubmitInvitation(e.principal, e.invitationID, e.token, request)
	if err != nil {
		return relayFailure(request, "invite_denied", err)
	}
	response, err := e.store.Wait(ctx, task.ID, operationTimeout(request.Operation))
	if err != nil {
		return relayFailure(request, "relay_timeout", err)
	}
	return response
}

func (e relayExecutor) Execute(ctx context.Context, request protocol.Request) protocol.Response {
	task, err := e.store.Submit(e.principal, e.deviceID, request)
	if err != nil {
		return relayFailure(request, "relay_denied", err)
	}
	response, err := e.store.Wait(ctx, task.ID, operationTimeout(request.Operation))
	if err != nil {
		return relayFailure(request, "relay_timeout", err)
	}
	return response
}

func operationTimeout(operation protocol.Operation) time.Duration {
	timeout := 15 * time.Second
	if definition, ok := protocol.DefinitionFor(operation); ok && definition.Timeout > 0 {
		timeout = definition.Timeout + 5*time.Second
	}
	return timeout
}

func relayFailure(request protocol.Request, code string, err error) protocol.Response {
	return protocol.Response{Version: protocol.Version, RequestID: request.RequestID, Status: protocol.StatusError,
		Error: &protocol.Error{Code: code, Message: err.Error()}}
}

func (h *Handler) protectedResource(w http.ResponseWriter, r *http.Request) {
	resource := h.resourceURL(r) + "/mcp"
	issuer := strings.TrimRight(strings.TrimSpace(os.Getenv("SESSIONSIFT_ACCESS_ISSUER")), "/")
	servers := []string{}
	if issuer != "" {
		servers = append(servers, issuer)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"resource": resource, "authorization_servers": servers,
		"scopes_supported":       []string{"openid", "profile", "email", "offline_access", "session.read"},
		"resource_documentation": h.resourceURL(r) + "/docs/mcp",
	})
}

func (h *Handler) resourceURL(r *http.Request) string {
	if h.publicURL != "" {
		return h.publicURL
	}
	return "https://" + r.Host
}

func (h *Handler) allowEnrollment(key string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	if len(h.attempts) >= 4096 {
		for source, entry := range h.attempts {
			if now.Sub(entry.started) >= 10*time.Minute {
				delete(h.attempts, source)
			}
		}
		if _, exists := h.attempts[key]; !exists && len(h.attempts) >= 4096 {
			return false
		}
	}
	window := h.attempts[key]
	if window.started.IsZero() || now.Sub(window.started) >= 10*time.Minute {
		h.attempts[key] = attemptWindow{started: now, count: 1}
		return true
	}
	if window.count >= 8 {
		return false
	}
	window.count++
	h.attempts[key] = window
	return true
}

func remoteIP(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); net.ParseIP(value) != nil {
		return value
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, bridgeRequestLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return false
	}
	return true
}

func decodeBytes(w http.ResponseWriter, body []byte, target any) bool {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return false
	}
	return true
}

func writeResult(w http.ResponseWriter, status int, value any, err error) {
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, context.DeadlineExceeded) {
			code = http.StatusGatewayTimeout
		}
		writeError(w, code, err.Error())
		return
	}
	writeJSON(w, status, value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func securityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", hostedCSP())
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
}
