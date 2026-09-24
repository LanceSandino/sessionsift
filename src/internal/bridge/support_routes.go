// @module BridgeSupportRoutes serves the supporter side of code-based connections:
// enter a code, get connected to exactly one session, leave at any time.
package bridge

import (
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/accessauth"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
)

const (
	supportCookiePrefix = "__Host-sessionsift_support_"
	supportRedeemWait   = 20 * time.Second
)

var supportPathPattern = regexp.MustCompile(`^/support/(support_[a-f0-9]{16,64})(?:/(metadata|protocol|disconnect))?$`)

func isSupportPath(path string) bool {
	return path == "/support" || strings.HasPrefix(path, "/support/")
}

func supportCookieName(id string) string {
	return supportCookiePrefix + strings.TrimPrefix(id, "support_")
}

func (h *Handler) supportRoute(w http.ResponseWriter, r *http.Request, principal accessauth.Principal) {
	switch {
	case r.Method == http.MethodGet && (r.URL.Path == "/support" || r.URL.Path == "/support/"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// The entry form has no rich content; the hosted bundle's console
		// layout would otherwise stretch its card to full width.
		_, _ = io.WriteString(w, supportPage)
		return
	case r.Method == http.MethodPost && r.URL.Path == "/support/connect":
		h.connectSupport(w, r, principal)
		return
	}
	match := supportPathPattern.FindStringSubmatch(r.URL.Path)
	if match == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	id, action := match[1], match[2]
	cookie, err := r.Cookie(supportCookieName(id))
	if err != nil || cookie.Value == "" {
		writeError(w, http.StatusUnauthorized, "support session required")
		return
	}
	token := cookie.Value
	switch {
	case action == "" && r.Method == http.MethodGet:
		if _, err = h.store.supportSession(principal.Email, id, token); err != nil {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, hostedPageAt(invitePage, "/support/"))
	case action == "metadata" && r.Method == http.MethodGet:
		support, supportErr := h.store.supportSession(principal.Email, id, token)
		if supportErr != nil {
			writeError(w, http.StatusForbidden, supportErr.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": support.ID, "kind": "support", "session_id": support.SessionID, "role": support.Role,
			"operations": support.Operations, "invitee": support.Supporter, "expires_at": support.AccessExpiresAt})
	case action == "protocol" && r.Method == http.MethodPost:
		if !sameOriginRequest(r, h.resourceURL(r)) {
			writeError(w, http.StatusForbidden, "cross-site request denied")
			return
		}
		var request protocol.Request
		if !decodeJSON(w, r, &request) {
			return
		}
		task, submitErr := h.store.SubmitSupport(principal.Email, id, token, request)
		if submitErr != nil {
			writeJSON(w, http.StatusOK, relayFailure(request, "support_denied", submitErr))
			return
		}
		response, waitErr := h.store.Wait(r.Context(), task.ID, operationTimeout(request.Operation))
		if waitErr != nil {
			writeJSON(w, http.StatusOK, relayFailure(request, "relay_timeout", waitErr))
			return
		}
		writeJSON(w, http.StatusOK, response)
	case action == "disconnect" && r.Method == http.MethodPost:
		if !sameOriginRequest(r, h.resourceURL(r)) {
			writeError(w, http.StatusForbidden, "cross-site request denied")
			return
		}
		endErr := h.store.EndSupportBySupporter(principal.Email, id, token)
		http.SetCookie(w, &http.Cookie{Name: supportCookieName(id), Value: "", Path: "/", MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		writeResult(w, http.StatusOK, map[string]bool{"disconnected": endErr == nil}, endErr)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

// connectSupport redeems an entered code: the bridge finds the issuing device
// by code hash, the device verifies the code and binds the supporter, and only
// then does the browser receive a session cookie for that one session.
func (h *Handler) connectSupport(w http.ResponseWriter, r *http.Request, principal accessauth.Principal) {
	if !sameOriginRequest(r, h.resourceURL(r)) {
		writeError(w, http.StatusForbidden, "cross-site request denied")
		return
	}
	if !h.allowAttempt("connect-ip:"+remoteIP(r)) || !h.allowAttempt("connect-id:"+principal.Email) {
		w.Header().Set("Retry-After", "600")
		writeError(w, http.StatusTooManyRequests, "too many connection attempts; wait ten minutes")
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	support, err := h.store.FindWaitingSupport(body.Code)
	if err != nil {
		writeError(w, http.StatusNotFound, "that code is not valid or has expired; ask for a new one")
		return
	}
	task, err := h.store.QueueSupportRedeem(principal.Email, support, body.Code)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "the source device is busy; try again")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), supportRedeemWait)
	defer cancel()
	response, err := h.store.Wait(ctx, task.ID, supportRedeemWait)
	if err != nil {
		status, message := http.StatusGatewayTimeout, "the source device did not respond; make sure SessionSift is running there"
		if errors.Is(err, context.Canceled) {
			status, message = http.StatusRequestTimeout, "connection attempt was cancelled"
		}
		writeError(w, status, message)
		return
	}
	activated, token, err := h.store.ActivateSupport(support.ID, principal.Email, response)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	maxAge := int(time.Until(*activated.AccessExpiresAt).Seconds())
	http.SetCookie(w, &http.Cookie{Name: supportCookieName(activated.ID), Value: token, Path: "/", MaxAge: maxAge, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]any{"redirect": "/support/" + activated.ID, "session_id": activated.SessionID, "role": activated.Role, "expires_at": activated.AccessExpiresAt})
}
