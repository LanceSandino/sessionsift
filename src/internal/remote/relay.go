// @module SecureRelay implements a provider-neutral typed relay with signed device responses.
package remote

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/agent/service"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
)

type RelayTask struct {
	ID         string           `json:"id"`
	DeviceID   string           `json:"device_id"`
	GrantToken string           `json:"grant_token"`
	Request    protocol.Request `json:"request"`
	CreatedAt  time.Time        `json:"created_at"`
}
type relayResult struct {
	response protocol.Response
	ready    chan struct{}
	deviceID string
	done     bool
}
type Relay struct {
	mu      sync.Mutex
	devices map[string]ed25519.PublicKey
	queues  map[string][]RelayTask
	results map[string]*relayResult
}

func NewRelay() *Relay {
	return &Relay{devices: map[string]ed25519.PublicKey{}, queues: map[string][]RelayTask{}, results: map[string]*relayResult{}}
}
func (r *Relay) Register(deviceID string, publicKey ed25519.PublicKey) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.devices[deviceID] = append(ed25519.PublicKey(nil), publicKey...)
}
func (r *Relay) Submit(deviceID, grantToken string, request protocol.Request) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.devices[deviceID]; !ok {
		return "", errors.New("device is not registered")
	}
	id := "relay_" + shortHash(deviceID+request.RequestID+time.Now().UTC().String())
	task := RelayTask{ID: id, DeviceID: deviceID, GrantToken: grantToken, Request: request, CreatedAt: time.Now().UTC()}
	r.queues[deviceID] = append(r.queues[deviceID], task)
	r.results[id] = &relayResult{ready: make(chan struct{}), deviceID: deviceID}
	return id, nil
}
func (r *Relay) Next(deviceID string) (RelayTask, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	queue := r.queues[deviceID]
	if len(queue) == 0 {
		return RelayTask{}, false
	}
	task := queue[0]
	r.queues[deviceID] = queue[1:]
	return task, true
}
func (r *Relay) Complete(taskID string, response protocol.Response, signature []byte) error {
	encoded, _ := json.Marshal(response)
	return r.CompleteEncoded(taskID, encoded, signature)
}

func (r *Relay) CompleteEncoded(taskID string, encoded []byte, signature []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	result, ok := r.results[taskID]
	if !ok {
		return errors.New("relay task not found")
	}
	key, registered := r.devices[result.deviceID]
	if !registered || !Verify(key, encoded, signature) {
		return errors.New("invalid device signature")
	}
	if result.done {
		return errors.New("relay task already completed")
	}
	var response protocol.Response
	if json.Unmarshal(encoded, &response) != nil {
		return errors.New("invalid relay response")
	}
	result.response = response
	result.done = true
	close(result.ready)
	return nil
}
func (r *Relay) Wait(ctx context.Context, taskID string) (protocol.Response, error) {
	r.mu.Lock()
	result, ok := r.results[taskID]
	r.mu.Unlock()
	if !ok {
		return protocol.Response{}, errors.New("relay task not found")
	}
	select {
	case <-ctx.Done():
		return protocol.Response{}, ctx.Err()
	case <-result.ready:
		return result.response, nil
	}
}

type Agent struct {
	identity   Identity
	manager    *Manager
	repository service.Repository
}

func NewAgent(identity Identity, manager *Manager, repository service.Repository) *Agent {
	return &Agent{identity: identity, manager: manager, repository: repository}
}

type GrantAuthorizer struct {
	manager *Manager
	token   string
}

// PrincipalAuthorizer applies a locally approved grant to an edge-authenticated identity.
type PrincipalAuthorizer struct {
	manager   *Manager
	principal string
}

func NewPrincipalAuthorizer(manager *Manager, principal string) PrincipalAuthorizer {
	return PrincipalAuthorizer{manager: manager, principal: principal}
}

func (authorizer PrincipalAuthorizer) Authorize(ctx context.Context, request protocol.Request) error {
	return authorizer.manager.AuthorizePrincipalRequest(ctx, authorizer.principal, request)
}

func NewGrantAuthorizer(manager *Manager, token string) GrantAuthorizer {
	return GrantAuthorizer{manager: manager, token: token}
}
func (a GrantAuthorizer) Authorize(ctx context.Context, request protocol.Request) error {
	return a.manager.Authorize(ctx, a.token, request.Operation)
}
func (a *Agent) ServeNext(ctx context.Context, relay *Relay) (bool, error) {
	task, ok := relay.Next(a.identity.ID)
	if !ok {
		return false, nil
	}
	response := a.executeTask(ctx, task)
	encoded, _ := json.Marshal(response)
	return true, relay.Complete(task.ID, response, a.identity.Sign(encoded))
}
func targetID(arguments json.RawMessage) string {
	var value map[string]any
	if json.Unmarshal(arguments, &value) != nil {
		return ""
	}
	for _, key := range []string{"session_id", "turn_id", "a"} {
		if text, ok := value[key].(string); ok {
			return text
		}
	}
	return ""
}

type RelayExecutor struct {
	Relay      *Relay
	DeviceID   string
	GrantToken string
	Timeout    time.Duration
}

func (e RelayExecutor) Execute(ctx context.Context, request protocol.Request) protocol.Response {
	taskID, err := e.Relay.Submit(e.DeviceID, e.GrantToken, request)
	if err != nil {
		return protocol.Response{Version: protocol.Version, RequestID: request.RequestID, Status: protocol.StatusError, Error: &protocol.Error{Code: "relay_error", Message: err.Error()}}
	}
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	response, err := e.Relay.Wait(waitCtx, taskID)
	if err != nil {
		return protocol.Response{Version: protocol.Version, RequestID: request.RequestID, Status: protocol.StatusError, Error: &protocol.Error{Code: "relay_timeout", Message: err.Error()}}
	}
	return response
}
