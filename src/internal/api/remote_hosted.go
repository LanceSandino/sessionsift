// @module RemoteHostedAPI gives the owner UI control of the bridge connection and code-based support.
package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/remote"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

// hostedRoute serves owner-only bridge and support routes. It reports whether
// it handled the request so remoteRoute can fall through to other routes.
func hostedRoute(store *storage.Store, hosted *remote.HostedRuntime, w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if !strings.HasPrefix(path, "/api/v1/remote/bridge") && !strings.HasPrefix(path, "/api/v1/remote/support") {
		return false
	}
	if hosted == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "bridge connections are not available in this process"})
		return true
	}
	switch {
	case r.Method == http.MethodGet && path == "/api/v1/remote/bridge":
		writeJSON(w, http.StatusOK, hosted.Status())
	case r.Method == http.MethodPost && path == "/api/v1/remote/bridge/connect":
		var body struct {
			URL  string `json:"url"`
			Name string `json:"name"`
		}
		if !decodeRemote(w, r, &body) {
			return true
		}
		status, err := hosted.Connect(r.Context(), body.URL, body.Name)
		remoteWrite(w, http.StatusOK, status, err)
	case r.Method == http.MethodPost && path == "/api/v1/remote/bridge/link":
		code, expires, err := hosted.StartLink(r.Context())
		remoteWrite(w, http.StatusCreated, map[string]any{"code": code, "expires_at": expires, "link_url": hosted.Status().URL + "/"}, err)
	case r.Method == http.MethodPost && path == "/api/v1/remote/bridge/refresh":
		status, err := hosted.RefreshOwner(r.Context())
		remoteWrite(w, http.StatusOK, status, err)
	case r.Method == http.MethodGet && path == "/api/v1/remote/support":
		writeJSON(w, http.StatusOK, hosted.SupportSessions(r.Context()))
	case r.Method == http.MethodPost && path == "/api/v1/remote/support":
		var body struct {
			SessionID string `json:"session_id"`
			Role      string `json:"role"`
			Minutes   int    `json:"minutes"`
		}
		if !decodeRemote(w, r, &body) {
			return true
		}
		session, err := store.GetSession(r.Context(), body.SessionID)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return true
		}
		offer, err := hosted.StartSupport(r.Context(), session.ID, session.Title, body.Role, time.Duration(body.Minutes)*time.Minute)
		remoteWrite(w, http.StatusCreated, offer, err)
	case r.Method == http.MethodPost && path == "/api/v1/remote/support/end":
		var body struct {
			ID string `json:"id"`
		}
		if !decodeRemote(w, r, &body) {
			return true
		}
		support, err := hosted.EndSupport(r.Context(), body.ID)
		remoteWrite(w, http.StatusOK, support, err)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
	return true
}
