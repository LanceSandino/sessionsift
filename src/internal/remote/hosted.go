// @module HostedAgent is the device's signed bridge client: registration, grants, invitations, and task serving.
package remote

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/agent/service"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
)

// HostedConfig records this device's bridge registration. Owner is empty
// until an owner links the device by entering a code it showed.
type HostedConfig struct {
	URL      string `json:"url"`
	DeviceID string `json:"device_id"`
	Owner    string `json:"owner,omitempty"`
	Name     string `json:"name,omitempty"`
}

type HostedInvitation struct {
	ID           string               `json:"id"`
	DeviceID     string               `json:"device_id"`
	Owner        string               `json:"owner"`
	Invitee      string               `json:"invitee"`
	SessionID    string               `json:"session_id"`
	Role         string               `json:"role"`
	Operations   []protocol.Operation `json:"operations"`
	CreatedAt    time.Time            `json:"created_at"`
	ExpiresAt    time.Time            `json:"expires_at"`
	AcceptedAt   *time.Time           `json:"accepted_at,omitempty"`
	LastViewedAt *time.Time           `json:"last_viewed_at,omitempty"`
	RevokedAt    *time.Time           `json:"revoked_at,omitempty"`
	AcceptURL    string               `json:"accept_url,omitempty"`
}

type HostedInviter struct {
	client   *http.Client
	config   HostedConfig
	identity Identity
	manager  *Manager
}

func NewHostedInviter(client *http.Client, config HostedConfig, identity Identity, manager *Manager) *HostedInviter {
	return &HostedInviter{client: client, config: config, identity: identity, manager: manager}
}

func (i *HostedInviter) Create(ctx context.Context, invitee, sessionID, role string, duration time.Duration) (HostedInvitation, error) {
	if i == nil || i.manager == nil || duration <= 0 || duration > 30*24*time.Hour {
		return HostedInvitation{}, errors.New("hosted invitations are not configured or duration is invalid")
	}
	if i.config.Owner == "" {
		return HostedInvitation{}, errors.New("link this device to an owner account to send email invitations, or share a support code instead")
	}
	expiresAt := time.Now().UTC().Add(duration)
	body, _ := json.Marshal(map[string]any{"owner": i.config.Owner, "invitee": invitee, "session_id": sessionID, "role": role, "expires_at": expiresAt})
	response, err := signedHostedRequest(ctx, i.client, i.config, i.identity, http.MethodPost, "/agent/v1/invitations", body)
	if err != nil {
		return HostedInvitation{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		return HostedInvitation{}, hostedResponseError(response)
	}
	var envelope struct {
		Invitation HostedInvitation `json:"invitation"`
		AcceptURL  string           `json:"accept_url"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&envelope); err != nil {
		return HostedInvitation{}, err
	}
	envelope.Invitation.AcceptURL = envelope.AcceptURL
	if envelope.Invitation.DeviceID != i.config.DeviceID || envelope.Invitation.SessionID != sessionID || envelope.Invitation.Role != role || envelope.Invitation.Invitee != strings.ToLower(strings.TrimSpace(invitee)) {
		_ = i.revokeBridge(ctx, envelope.Invitation.ID)
		return HostedInvitation{}, errors.New("bridge returned a mismatched invitation")
	}
	if _, err = i.manager.ApproveInvitePrincipal(envelope.Invitation.ID, envelope.Invitation.Invitee, sessionID, role, envelope.Invitation.ExpiresAt); err != nil {
		_ = i.revokeBridge(ctx, envelope.Invitation.ID)
		return HostedInvitation{}, err
	}
	return envelope.Invitation, nil
}

func (i *HostedInviter) List(ctx context.Context) ([]HostedInvitation, error) {
	if i == nil {
		return nil, errors.New("hosted invitations are not configured")
	}
	response, err := signedHostedRequest(ctx, i.client, i.config, i.identity, http.MethodGet, "/agent/v1/invitations", nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, hostedResponseError(response)
	}
	values := make([]HostedInvitation, 0)
	if err = json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&values); err != nil {
		return nil, err
	}
	return values, nil
}

func (i *HostedInviter) Revoke(ctx context.Context, invitationID string) error {
	if i == nil || i.manager == nil {
		return errors.New("hosted invitations are not configured")
	}
	if err := i.revokeBridge(ctx, invitationID); err != nil {
		return err
	}
	return i.manager.RevokeInvitation(invitationID)
}

func (i *HostedInviter) revokeBridge(ctx context.Context, invitationID string) error {
	path := "/agent/v1/invitations/" + url.PathEscape(invitationID)
	response, err := signedHostedRequest(ctx, i.client, i.config, i.identity, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return hostedResponseError(response)
	}
	return nil
}

func LoadHostedConfig(path string) (HostedConfig, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return HostedConfig{}, err
	}
	var config HostedConfig
	if json.Unmarshal(body, &config) != nil {
		return HostedConfig{}, errors.New("invalid hosted bridge configuration")
	}
	if _, err = validateBridgeURL(config.URL); err != nil || config.DeviceID == "" {
		return HostedConfig{}, errors.New("invalid hosted bridge configuration")
	}
	return config, nil
}

func SaveHostedConfig(path string, config HostedConfig) error {
	parsed, err := validateBridgeURL(config.URL)
	if err != nil || config.DeviceID == "" {
		return errors.New("invalid hosted bridge configuration")
	}
	config.URL = strings.TrimRight(parsed.String(), "/")
	body, _ := json.MarshalIndent(config, "", "  ")
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err = os.WriteFile(temporary, append(body, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// RegisterHosted joins a bridge with key-possession proof. No account or code
// is needed; the returned configuration has no owner until the device is linked.
func RegisterHosted(ctx context.Context, client *http.Client, bridgeURL, name string, identity Identity) (HostedConfig, error) {
	parsed, err := validateBridgeURL(bridgeURL)
	if err != nil {
		return HostedConfig{}, err
	}
	publicKey := base64.RawURLEncoding.EncodeToString(identity.PublicKey)
	message, _ := json.Marshal([]string{"sessionsift.register.v1", name, publicKey})
	body, _ := json.Marshal(map[string]string{"name": name, "public_key": publicKey, "proof": base64.RawURLEncoding.EncodeToString(identity.Sign(message))})
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(parsed.String(), "/")+"/agent/v1/register", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response, err := hostedClient(client).Do(request)
	if err != nil {
		return HostedConfig{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		return HostedConfig{}, hostedResponseError(response)
	}
	var device HostedDevice
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&device); err != nil {
		return HostedConfig{}, err
	}
	if device.ID != identity.ID {
		return HostedConfig{}, errors.New("bridge returned an unexpected device identity")
	}
	return HostedConfig{URL: strings.TrimRight(parsed.String(), "/"), DeviceID: device.ID, Owner: device.Owner, Name: device.Name}, nil
}

func RegisterHostedGrant(ctx context.Context, client *http.Client, config HostedConfig, identity Identity, principal string, operations []protocol.Operation, expiresAt time.Time) error {
	body, _ := json.Marshal(map[string]any{"principal": principal, "operations": operations, "expires_at": expiresAt.UTC()})
	response, err := signedHostedRequest(ctx, client, config, identity, http.MethodPost, "/agent/v1/grants", body)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		return hostedResponseError(response)
	}
	return nil
}

func RevokeHostedGrant(ctx context.Context, client *http.Client, config HostedConfig, identity Identity, principal string) error {
	body, _ := json.Marshal(map[string]string{"principal": principal})
	response, err := signedHostedRequest(ctx, client, config, identity, http.MethodDelete, "/agent/v1/grants", body)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return hostedResponseError(response)
	}
	return nil
}

func (a *Agent) ServeHostedNext(ctx context.Context, client *http.Client, config HostedConfig) (bool, error) {
	response, err := signedHostedRequest(ctx, client, config, a.identity, http.MethodGet, "/agent/v1/tasks/next", nil)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return false, nil
	}
	if response.StatusCode != http.StatusOK {
		return false, hostedResponseError(response)
	}
	var task struct {
		ID        string           `json:"id"`
		Principal string           `json:"principal"`
		Request   protocol.Request `json:"request"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&task); err != nil {
		return false, err
	}
	result := a.executeHostedTask(ctx, task.Principal, task.Request)
	encoded, _ := json.Marshal(result)
	a.manager.RecordAudit(AuditRecord{ClientID: task.Principal, Operation: task.Request.Operation, TargetID: targetID(task.Request.Arguments), Allowed: result.Status != protocol.StatusDenied, ResponseBytes: len(encoded), Status: "hosted_bridge"})
	completion, _ := json.Marshal(map[string]string{
		"task_id": task.ID, "response": base64.RawURLEncoding.EncodeToString(encoded),
		"signature": base64.RawURLEncoding.EncodeToString(a.identity.Sign(encoded)),
	})
	completeResponse, err := signedHostedRequest(ctx, client, config, a.identity, http.MethodPost, "/agent/v1/tasks/complete", completion)
	if err != nil {
		return false, err
	}
	defer completeResponse.Body.Close()
	if completeResponse.StatusCode != http.StatusOK {
		return false, hostedResponseError(completeResponse)
	}
	return true, nil
}

func (a *Agent) executeHostedTask(ctx context.Context, principal string, request protocol.Request) protocol.Response {
	if request.Operation == protocol.SupportRedeem {
		return a.redeemSupport(principal, request)
	}
	authorizer := NewPrincipalAuthorizer(a.manager, principal)
	if request.Operation != protocol.AnnotationList && request.Operation != protocol.AnnotationCreate && request.Operation != protocol.ReviewList && request.Operation != protocol.ReviewCreate {
		return service.New(a.repository, authorizer).Execute(ctx, request)
	}
	response := protocol.Response{Version: protocol.Version, RequestID: request.RequestID}
	if err := request.Validate(); err != nil {
		response.Status, response.Error = protocol.StatusInvalidRequest, &protocol.Error{Code: "invalid_request", Message: err.Error()}
		return response
	}
	if err := authorizer.Authorize(ctx, request); err != nil {
		response.Status, response.Error = protocol.StatusDenied, &protocol.Error{Code: "denied", Message: err.Error()}
		return response
	}
	var arguments struct {
		SessionID string `json:"session_id"`
		TurnID    string `json:"turn_id"`
		Body      string `json:"body"`
		Status    string `json:"status"`
		Summary   string `json:"summary"`
	}
	if json.Unmarshal(request.Arguments, &arguments) != nil || arguments.SessionID == "" {
		response.Status, response.Error = protocol.StatusInvalidRequest, &protocol.Error{Code: "invalid_arguments", Message: "session_id is required"}
		return response
	}
	var data any
	var err error
	switch request.Operation {
	case protocol.AnnotationList:
		data = a.manager.Annotations(arguments.SessionID)
	case protocol.AnnotationCreate:
		data, err = a.manager.AddAnnotation(arguments.SessionID, arguments.TurnID, principal, arguments.Body)
	case protocol.ReviewList:
		data = a.manager.Reviews(arguments.SessionID)
	case protocol.ReviewCreate:
		data, err = a.manager.AddReview(arguments.SessionID, principal, arguments.Status, arguments.Summary)
	}
	if err != nil {
		response.Status, response.Error = protocol.StatusInvalidRequest, &protocol.Error{Code: "invalid_arguments", Message: err.Error()}
		return response
	}
	response.Status, response.Data = protocol.StatusOK, data
	return response
}

func RunHostedLoop(ctx context.Context, agent *Agent, client *http.Client, config HostedConfig, onError func(error)) {
	for {
		worked, err := agent.ServeHostedNext(ctx, client, config)
		if err != nil && ctx.Err() == nil && onError != nil {
			onError(err)
		}
		delay := 250 * time.Millisecond
		if err != nil {
			delay = 3 * time.Second
		} else if worked {
			delay = 10 * time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func signedHostedRequest(ctx context.Context, client *http.Client, config HostedConfig, identity Identity, method, path string, body []byte) (*http.Response, error) {
	if config.DeviceID != identity.ID {
		return nil, errors.New("bridge configuration does not match this device identity")
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(config.URL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	digest := sha256.Sum256(body)
	message := []byte(method + "\n" + path + "\n" + timestamp + "\n" + hex.EncodeToString(digest[:]))
	request.Header.Set("X-SessionSift-Device", identity.ID)
	request.Header.Set("X-SessionSift-Timestamp", timestamp)
	request.Header.Set("X-SessionSift-Signature", base64.RawURLEncoding.EncodeToString(identity.Sign(message)))
	return hostedClient(client).Do(request)
}

func validateBridgeURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "https" && !(parsed.Scheme == "http" && (parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost"))) {
		return nil, errors.New("bridge URL must be HTTPS (HTTP is allowed only for localhost)")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed, nil
}

func hostedClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: 25 * time.Second}
}

func hostedResponseError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	var value struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &value)
	if value.Error == "" {
		value.Error = strings.TrimSpace(string(body))
	}
	if value.Error == "" {
		value.Error = response.Status
	}
	return fmt.Errorf("bridge request failed: %s", value.Error)
}
