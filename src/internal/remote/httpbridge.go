// Package remote provides a provider-neutral HTTPS bridge transport.
// @module HTTPBridge lets the Local Agent poll outbound without exposing the workstation.
package remote

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/agent/service"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
)

const bridgeBodyLimit = 10 << 20

type bridgeHandler struct{ relay *Relay }

// NewBridgeHandler exposes the small typed relay API. Deploy it only behind HTTPS.
func NewBridgeHandler(relay *Relay) http.Handler { return bridgeHandler{relay: relay} }

func (h bridgeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/tasks":
		var body struct {
			DeviceID   string           `json:"device_id"`
			GrantToken string           `json:"grant_token"`
			Request    protocol.Request `json:"request"`
		}
		if !bridgeDecode(w, r, &body) {
			return
		}
		id, err := h.relay.Submit(body.DeviceID, body.GrantToken, body.Request)
		bridgeWrite(w, http.StatusAccepted, map[string]string{"task_id": id}, err)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/device/next":
		deviceID, ok := h.authenticateDevice(w, r)
		if !ok {
			return
		}
		task, found := h.relay.Next(deviceID)
		if !found {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		bridgeWrite(w, http.StatusOK, task, nil)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/device/complete":
		deviceID, ok := h.authenticateDevice(w, r)
		if !ok {
			return
		}
		var body struct {
			TaskID    string          `json:"task_id"`
			Response  json.RawMessage `json:"response"`
			Signature string          `json:"signature"`
		}
		if !bridgeDecode(w, r, &body) {
			return
		}
		if taskDevice, exists := h.relay.taskDevice(body.TaskID); exists && taskDevice != deviceID {
			bridgeWrite(w, http.StatusForbidden, nil, errors.New("task belongs to another device"))
			return
		}
		signature, err := base64.RawURLEncoding.DecodeString(body.Signature)
		if err == nil {
			err = h.relay.CompleteEncoded(body.TaskID, body.Response, signature)
		}
		bridgeWrite(w, http.StatusOK, map[string]bool{"completed": err == nil}, err)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/tasks/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/tasks/")
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		response, err := h.relay.Wait(ctx, id)
		bridgeWrite(w, http.StatusOK, response, err)
	default:
		bridgeWrite(w, http.StatusNotFound, nil, errors.New("not found"))
	}
}

func (h bridgeHandler) authenticateDevice(w http.ResponseWriter, r *http.Request) (string, bool) {
	deviceID := r.Header.Get("X-SessionSift-Device")
	timestamp := r.Header.Get("X-SessionSift-Timestamp")
	signatureText := r.Header.Get("X-SessionSift-Signature")
	key, ok := h.relay.deviceKey(deviceID)
	parsed, err := time.Parse(time.RFC3339Nano, timestamp)
	if !ok || err != nil || time.Since(parsed).Abs() > 5*time.Minute {
		bridgeWrite(w, http.StatusUnauthorized, nil, errors.New("device authentication failed"))
		return "", false
	}
	signature, err := base64.RawURLEncoding.DecodeString(signatureText)
	message := []byte(r.Method + "\n" + r.URL.Path + "\n" + timestamp)
	if err != nil || !ed25519.Verify(key, message, signature) {
		bridgeWrite(w, http.StatusUnauthorized, nil, errors.New("device authentication failed"))
		return "", false
	}
	return deviceID, true
}

func (r *Relay) deviceKey(deviceID string) (ed25519.PublicKey, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, ok := r.devices[deviceID]
	return append(ed25519.PublicKey(nil), key...), ok
}

func (r *Relay) taskDevice(taskID string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result, ok := r.results[taskID]
	if !ok {
		return "", false
	}
	return result.deviceID, true
}

func (a *Agent) ServeHTTPNext(ctx context.Context, client *http.Client, bridgeURL string) (bool, error) {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	endpoint := strings.TrimRight(bridgeURL, "/") + "/v1/device/next"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	a.signBridgeRequest(request)
	response, err := client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return false, nil
	}
	if response.StatusCode != http.StatusOK {
		return false, errors.New("bridge poll refused")
	}
	var task RelayTask
	if err = json.NewDecoder(io.LimitReader(response.Body, bridgeBodyLimit)).Decode(&task); err != nil {
		return false, err
	}
	result := a.executeTask(ctx, task)
	encoded, _ := json.Marshal(result)
	completion, _ := json.Marshal(map[string]any{"task_id": task.ID, "response": result, "signature": base64.RawURLEncoding.EncodeToString(a.identity.Sign(encoded))})
	completeRequest, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(bridgeURL, "/")+"/v1/device/complete", bytes.NewReader(completion))
	completeRequest.Header.Set("Content-Type", "application/json")
	a.signBridgeRequest(completeRequest)
	completeResponse, err := client.Do(completeRequest)
	if err != nil {
		return false, err
	}
	defer completeResponse.Body.Close()
	if completeResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(completeResponse.Body, 4096))
		return false, errors.New("bridge completion refused: " + strings.TrimSpace(string(body)))
	}
	return true, nil
}

func (a *Agent) signBridgeRequest(request *http.Request) {
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	message := []byte(request.Method + "\n" + request.URL.Path + "\n" + timestamp)
	request.Header.Set("X-SessionSift-Device", a.identity.ID)
	request.Header.Set("X-SessionSift-Timestamp", timestamp)
	request.Header.Set("X-SessionSift-Signature", base64.RawURLEncoding.EncodeToString(a.identity.Sign(message)))
}

func (a *Agent) executeTask(ctx context.Context, task RelayTask) protocol.Response {
	grant, _ := a.manager.GrantForToken(task.GrantToken)
	local := service.New(a.repository, NewGrantAuthorizer(a.manager, task.GrantToken))
	response := local.Execute(ctx, task.Request)
	encoded, _ := json.Marshal(response)
	a.manager.RecordAudit(AuditRecord{ClientID: grant.Reviewer, GrantID: grant.ID, Operation: task.Request.Operation, TargetID: targetID(task.Request.Arguments), Allowed: response.Status != protocol.StatusDenied, ResponseBytes: len(encoded), Status: string(response.Status)})
	return response
}

type HTTPRelayExecutor struct {
	Client     *http.Client
	BridgeURL  string
	DeviceID   string
	GrantToken string
}

func (e HTTPRelayExecutor) Execute(ctx context.Context, request protocol.Request) protocol.Response {
	client := e.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	body, _ := json.Marshal(map[string]any{"device_id": e.DeviceID, "grant_token": e.GrantToken, "request": request})
	response, err := client.Post(strings.TrimRight(e.BridgeURL, "/")+"/v1/tasks", "application/json", bytes.NewReader(body))
	if err != nil {
		return bridgeError(request, err)
	}
	defer response.Body.Close()
	var submitted struct {
		TaskID string `json:"task_id"`
	}
	if response.StatusCode != http.StatusAccepted || json.NewDecoder(response.Body).Decode(&submitted) != nil {
		return bridgeError(request, errors.New("bridge submission refused"))
	}
	waitURL := strings.TrimRight(e.BridgeURL, "/") + "/v1/tasks/" + url.PathEscape(submitted.TaskID)
	waitRequest, _ := http.NewRequestWithContext(ctx, http.MethodGet, waitURL, nil)
	waitResponse, err := client.Do(waitRequest)
	if err != nil {
		return bridgeError(request, err)
	}
	defer waitResponse.Body.Close()
	var result protocol.Response
	if waitResponse.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(waitResponse.Body, bridgeBodyLimit)).Decode(&result) != nil {
		return bridgeError(request, errors.New("bridge result unavailable"))
	}
	return result
}

func bridgeError(request protocol.Request, err error) protocol.Response {
	return protocol.Response{Version: protocol.Version, RequestID: request.RequestID, Status: protocol.StatusError, Error: &protocol.Error{Code: "relay_error", Message: err.Error()}}
}

func bridgeDecode(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, bridgeBodyLimit))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		bridgeWrite(w, http.StatusBadRequest, nil, errors.New("invalid request"))
		return false
	}
	return true
}

func bridgeWrite(w http.ResponseWriter, status int, value any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		if status < 400 {
			status = http.StatusBadRequest
		}
		value = map[string]string{"error": err.Error()}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
