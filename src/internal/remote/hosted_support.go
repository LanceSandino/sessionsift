// @module HostedSupport is the device side of code-based connections: it registers codes the
// source machine shows, verifies redemptions itself, and links the device to an owner.
package remote

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
	"github.com/LanceSandino/sessionsift/src/internal/supportcode"
)

// HostedDevice is the bridge's view of this device.
type HostedDevice struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Owner       string    `json:"owner"`
	Name        string    `json:"name"`
	LastSeen    time.Time `json:"last_seen"`
}

// HostedSupport is the bridge's routing record for one support code.
type HostedSupport struct {
	ID              string     `json:"id"`
	SessionID       string     `json:"session_id"`
	Supporter       string     `json:"supporter,omitempty"`
	ConnectedAt     *time.Time `json:"connected_at,omitempty"`
	AccessExpiresAt *time.Time `json:"access_expires_at,omitempty"`
	LastViewedAt    *time.Time `json:"last_viewed_at,omitempty"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
	EndReason       string     `json:"end_reason,omitempty"`
}

// FetchHostedDevice reads this device's registration, including its owner.
func FetchHostedDevice(ctx context.Context, client *http.Client, config HostedConfig, identity Identity) (HostedDevice, error) {
	var device HostedDevice
	err := hostedJSON(ctx, client, config, identity, http.MethodGet, "/agent/v1/device", nil, http.StatusOK, &device)
	if err == nil && device.ID != identity.ID {
		return HostedDevice{}, errors.New("bridge returned an unexpected device identity")
	}
	return device, err
}

// CreateHostedLink shows a code an owner enters in the hosted console to link
// this device to their account. Only the code's hash is sent.
func CreateHostedLink(ctx context.Context, client *http.Client, config HostedConfig, identity Identity) (string, time.Time, error) {
	code, err := supportcode.Generate()
	if err != nil {
		return "", time.Time{}, err
	}
	codeHash, _ := supportcode.Hash(supportcode.Link, code)
	expires := time.Now().UTC().Add(SupportCodeLifetime)
	body, _ := json.Marshal(map[string]any{"code_hash": codeHash, "expires_at": expires})
	if err = hostedJSON(ctx, client, config, identity, http.MethodPost, "/agent/v1/links", body, http.StatusCreated, nil); err != nil {
		return "", time.Time{}, err
	}
	return code, expires, nil
}

// CompleteOwnerLink adopts the owner the bridge reports after a link code was
// entered, and grants that owner the standard read capabilities locally and
// on the bridge. It returns the updated configuration and whether it changed.
func CompleteOwnerLink(ctx context.Context, client *http.Client, config HostedConfig, identity Identity, manager *Manager) (HostedConfig, bool, error) {
	device, err := FetchHostedDevice(ctx, client, config, identity)
	if err != nil {
		return config, false, err
	}
	if device.Owner == "" {
		return config, false, nil
	}
	if config.Owner != "" && device.Owner != config.Owner {
		return config, false, errors.New("bridge reports a different owner than this device recorded; disconnect and re-register")
	}
	ownerWasEmpty := config.Owner == ""
	config.Owner = device.Owner
	changed := config.WorkspaceID != device.WorkspaceID || config.Name != device.Name || ownerWasEmpty
	config.WorkspaceID, config.Name = device.WorkspaceID, device.Name
	if ownerWasEmpty {
		grant, grantErr := manager.ApproveOAuthPrincipal(device.Owner, protocol.ReadOperations(), 30*24*time.Hour)
		if grantErr != nil {
			return config, false, grantErr
		}
		if err = RegisterHostedGrant(ctx, client, config, identity, device.Owner, grant.Operations, grant.ExpiresAt); err != nil {
			_ = manager.Revoke(grant.ID)
			config.Owner = ""
			return config, false, err
		}
	}
	return config, changed, nil
}

// RegisterHostedSupport publishes the hash of a support code this device issued.
func RegisterHostedSupport(ctx context.Context, client *http.Client, config HostedConfig, identity Identity, support SupportSession, codeHash string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"id": support.ID, "code_hash": codeHash, "session_id": support.SessionID, "role": support.Role,
		"code_expires_at": support.CodeExpiresAt.UTC(), "access_minutes": support.AccessMinutes,
	})
	var envelope struct {
		ConnectURL string `json:"connect_url"`
	}
	if err := hostedJSON(ctx, client, config, identity, http.MethodPost, "/agent/v1/support", body, http.StatusCreated, &envelope); err != nil {
		return "", err
	}
	return envelope.ConnectURL, nil
}

// ListHostedSupport returns the bridge's routing state for this device's codes.
func ListHostedSupport(ctx context.Context, client *http.Client, config HostedConfig, identity Identity) ([]HostedSupport, error) {
	values := make([]HostedSupport, 0)
	err := hostedJSON(ctx, client, config, identity, http.MethodGet, "/agent/v1/support", nil, http.StatusOK, &values)
	return values, err
}

// RevokeHostedSupport stops the bridge routing a code or connection.
func RevokeHostedSupport(ctx context.Context, client *http.Client, config HostedConfig, identity Identity, id string) error {
	return hostedJSON(ctx, client, config, identity, http.MethodDelete, "/agent/v1/support/"+url.PathEscape(id), nil, http.StatusOK, nil)
}

func hostedJSON(ctx context.Context, client *http.Client, config HostedConfig, identity Identity, method, path string, body []byte, want int, target any) error {
	response, err := signedHostedRequest(ctx, client, config, identity, method, path, body)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != want {
		return hostedResponseError(response)
	}
	if target == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(target)
}

// redeemSupport verifies a relayed support code on this device. The response
// confirms only identifiers the bridge already routes; no session data leaves.
func (a *Agent) redeemSupport(principal string, request protocol.Request) protocol.Response {
	response := protocol.Response{Version: protocol.Version, RequestID: request.RequestID}
	var arguments struct {
		SupportID string `json:"support_id"`
		Code      string `json:"code"`
	}
	if err := request.Validate(); err != nil || json.Unmarshal(request.Arguments, &arguments) != nil || !strings.HasPrefix(arguments.SupportID, "support_") {
		response.Status, response.Error = protocol.StatusInvalidRequest, &protocol.Error{Code: "invalid_request", Message: "invalid support redemption"}
		return response
	}
	support, err := a.manager.RedeemSupport(arguments.SupportID, arguments.Code, principal)
	if err != nil {
		response.Status, response.Error = protocol.StatusDenied, &protocol.Error{Code: "denied", Message: err.Error()}
		return response
	}
	response.Status = protocol.StatusOK
	response.Data = map[string]any{"id": support.ID, "session_id": support.SessionID, "role": support.Role, "supporter": support.Supporter, "expires_at": support.ExpiresAt}
	return response
}
