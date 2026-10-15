// @module SyncHTTPTransport carries opaque objects over replay-protected signed agent requests.
package cloudsync

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/LanceSandino/sessionsift/src/internal/remote"
)

type HTTPTransport struct {
	Client   *http.Client
	Config   remote.HostedConfig
	Identity remote.Identity
}

type Usage struct {
	Objects int   `json:"objects"`
	Bytes   int64 `json:"bytes"`
	Limit   int64 `json:"limit"`
}

func (t *HTTPTransport) Usage(ctx context.Context) (Usage, error) {
	response, err := remote.SignedHostedRequest(ctx, t.Client, t.Config, t.Identity, http.MethodGet, "/agent/v1/sync/usage", nil)
	if err != nil {
		return Usage{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Usage{}, syncHTTPError(response)
	}
	var value Usage
	err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&value)
	return value, err
}

func (t *HTTPTransport) WorkspaceEpoch(ctx context.Context) (uint64, error) {
	response, err := remote.SignedHostedRequest(ctx, t.Client, t.Config, t.Identity, http.MethodGet, "/agent/v1/sync/workspace", nil)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, syncHTTPError(response)
	}
	var value struct {
		MinEpoch uint64 `json:"min_epoch"`
	}
	err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&value)
	if err == nil && value.MinEpoch == 0 {
		err = errors.New("sync workspace returned invalid key epoch")
	}
	return value.MinEpoch, err
}

func (t *HTTPTransport) DeleteWorkspace(ctx context.Context) (uint64, error) {
	body := []byte(`{"confirmation":"DELETE CLOUD COPIES"}`)
	response, err := remote.SignedHostedRequest(ctx, t.Client, t.Config, t.Identity, http.MethodDelete, "/agent/v1/sync/workspace", body)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, syncHTTPError(response)
	}
	var value struct {
		NextEpoch uint64 `json:"next_epoch"`
	}
	err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&value)
	if err == nil && value.NextEpoch == 0 {
		err = errors.New("sync deletion returned invalid key epoch")
	}
	return value.NextEpoch, err
}

func (t *HTTPTransport) DeviceKey(ctx context.Context, deviceID string) (ed25519.PublicKey, error) {
	response, err := remote.SignedHostedRequest(ctx, t.Client, t.Config, t.Identity, http.MethodGet, "/agent/v1/sync/devices/"+url.PathEscape(deviceID)+"/key", nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, syncHTTPError(response)
	}
	var value struct {
		DeviceID  string `json:"device_id"`
		PublicKey []byte `json:"public_key"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&value); err != nil || value.DeviceID != deviceID || len(value.PublicKey) != ed25519.PublicKeySize {
		return nil, errors.New("invalid sync source device key")
	}
	return ed25519.PublicKey(value.PublicKey), nil
}

func (t *HTTPTransport) Put(ctx context.Context, object WireObject, expected uint64) (Change, error) {
	body, err := json.Marshal(object)
	if err != nil {
		return Change{}, err
	}
	path := "/agent/v1/sync/objects/" + url.PathEscape(object.ObjectID) + "?expected_revision=" + strconv.FormatUint(expected, 10)
	response, err := remote.SignedHostedRequest(ctx, t.Client, t.Config, t.Identity, http.MethodPut, path, body)
	if err != nil {
		return Change{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK {
		return Change{}, syncHTTPError(response)
	}
	var change Change
	err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&change)
	return change, err
}

func (t *HTTPTransport) Changes(ctx context.Context, cursor int64, limit int) ([]Change, bool, error) {
	query := url.Values{"cursor": {strconv.FormatInt(cursor, 10)}, "limit": {strconv.Itoa(limit)}}.Encode()
	response, err := remote.SignedHostedRequest(ctx, t.Client, t.Config, t.Identity, http.MethodGet, "/agent/v1/sync/changes?"+query, nil)
	if err != nil {
		return nil, false, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, false, syncHTTPError(response)
	}
	var page struct {
		Changes []Change `json:"changes"`
		More    bool     `json:"more"`
	}
	err = json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&page)
	return page.Changes, page.More, err
}

func (t *HTTPTransport) Get(ctx context.Context, objectID string) (WireObject, error) {
	response, err := remote.SignedHostedRequest(ctx, t.Client, t.Config, t.Identity, http.MethodGet, "/agent/v1/sync/objects/"+url.PathEscape(objectID), nil)
	if err != nil {
		return WireObject{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return WireObject{}, syncHTTPError(response)
	}
	var object WireObject
	err = json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&object)
	return object, err
}

func syncHTTPError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	var envelope struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &envelope)
	if envelope.Error == "" {
		envelope.Error = response.Status
	}
	if response.StatusCode == http.StatusConflict {
		return fmt.Errorf("sync conflict: %s", envelope.Error)
	}
	if response.StatusCode == http.StatusRequestEntityTooLarge || response.StatusCode == http.StatusInsufficientStorage {
		return fmt.Errorf("sync quota: %s", envelope.Error)
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return fmt.Errorf("sync access denied: %s", envelope.Error)
	}
	if response.StatusCode == http.StatusNotFound {
		return errors.New("sync object or endpoint unavailable")
	}
	return fmt.Errorf("sync request failed: %s", envelope.Error)
}
