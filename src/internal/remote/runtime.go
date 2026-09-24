// @module HostedRuntime owns this device's live bridge connection: registration, the outbound
// polling loop, owner linking, invitations, and code-based support sessions.
package remote

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/agent/service"
)

// HostedRuntime lets the running Local Agent connect to a bridge without a
// restart and gives the owner UI one object for every hosted action.
type HostedRuntime struct {
	mu         sync.Mutex
	parent     context.Context
	path       string
	identity   Identity
	manager    *Manager
	repository service.Repository
	client     *http.Client
	onError    func(error)
	config     *HostedConfig
	cancel     context.CancelFunc
	// lastSupportSync rate-limits bridge support syncs (see dueForSupportSync).
	lastSupportSync time.Time
}

// RuntimeStatus is what the owner UI shows about the bridge connection.
type RuntimeStatus struct {
	Connected bool   `json:"connected"`
	URL       string `json:"url,omitempty"`
	DeviceID  string `json:"device_id"`
	Owner     string `json:"owner,omitempty"`
	Name      string `json:"name,omitempty"`
}

// NewHostedRuntime loads any saved registration and starts outbound polling.
func NewHostedRuntime(parent context.Context, path string, identity Identity, manager *Manager, repository service.Repository, client *http.Client, onError func(error)) (*HostedRuntime, error) {
	runtime := &HostedRuntime{parent: parent, path: path, identity: identity, manager: manager, repository: repository, client: client, onError: onError}
	config, err := LoadHostedConfig(path)
	if errors.Is(err, os.ErrNotExist) {
		return runtime, nil
	}
	if err != nil {
		return runtime, err
	}
	runtime.startLocked(config)
	return runtime, nil
}

// OpenHostedClient loads the saved registration for one-shot CLI commands
// without starting a polling loop (the running Local Agent owns the loop).
func OpenHostedClient(path string, identity Identity, manager *Manager, client *http.Client) (*HostedRuntime, error) {
	runtime := &HostedRuntime{path: path, identity: identity, manager: manager, client: client}
	config, err := LoadHostedConfig(path)
	if err == nil {
		runtime.config = &config
	} else if !errors.Is(err, os.ErrNotExist) {
		return runtime, err
	}
	return runtime, nil
}

func (r *HostedRuntime) startLocked(config HostedConfig) {
	if r.cancel != nil {
		r.cancel()
	}
	if r.parent == nil {
		r.config, r.cancel = &config, nil
		return
	}
	ctx, cancel := context.WithCancel(r.parent)
	r.config, r.cancel = &config, cancel
	go RunHostedLoop(ctx, NewAgent(r.identity, r.manager, r.repository), r.client, config, r.onError)
}

// Stop ends outbound polling.
func (r *HostedRuntime) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
}

// Status reports the current registration without contacting the bridge.
func (r *HostedRuntime) Status() RuntimeStatus {
	if r == nil {
		return RuntimeStatus{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	status := RuntimeStatus{DeviceID: r.identity.ID}
	if r.config != nil {
		status.Connected, status.URL, status.Owner, status.Name = true, r.config.URL, r.config.Owner, r.config.Name
	}
	return status
}

func (r *HostedRuntime) current() (HostedConfig, error) {
	if r == nil {
		return HostedConfig{}, errors.New("this device is not connected to a SessionSift bridge")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.config == nil {
		return HostedConfig{}, errors.New("this device is not connected to a SessionSift bridge")
	}
	return *r.config, nil
}

// Connect registers with a bridge, saves the registration and starts polling.
func (r *HostedRuntime) Connect(ctx context.Context, bridgeURL, name string) (RuntimeStatus, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = defaultDeviceName()
	}
	config, err := RegisterHosted(ctx, r.client, bridgeURL, name, r.identity)
	if err != nil {
		return RuntimeStatus{}, err
	}
	r.mu.Lock()
	if r.config != nil && r.config.URL == config.URL && config.Owner == "" {
		config.Owner = r.config.Owner
	}
	if err = SaveHostedConfig(r.path, config); err != nil {
		r.mu.Unlock()
		return RuntimeStatus{}, err
	}
	r.startLocked(config)
	r.mu.Unlock()
	return r.Status(), nil
}

// StartLink shows a code that links this device to an owner account.
func (r *HostedRuntime) StartLink(ctx context.Context) (string, time.Time, error) {
	config, err := r.current()
	if err != nil {
		return "", time.Time{}, err
	}
	return CreateHostedLink(ctx, r.client, config, r.identity)
}

// RefreshOwner completes an owner link after the owner entered the code.
func (r *HostedRuntime) RefreshOwner(ctx context.Context) (RuntimeStatus, error) {
	config, err := r.current()
	if err != nil {
		return r.Status(), err
	}
	updated, changed, err := CompleteOwnerLink(ctx, r.client, config, r.identity, r.manager)
	if err != nil || !changed {
		return r.Status(), err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err = SaveHostedConfig(r.path, updated); err != nil {
		return RuntimeStatus{}, err
	}
	r.startLocked(updated)
	status := RuntimeStatus{Connected: true, DeviceID: r.identity.ID, URL: updated.URL, Owner: updated.Owner, Name: updated.Name}
	return status, nil
}

// Inviter returns email-invitation support for the current registration.
func (r *HostedRuntime) Inviter() *HostedInviter {
	config, err := r.current()
	if err != nil {
		return nil
	}
	return NewHostedInviter(r.client, config, r.identity, r.manager)
}

// SupportOffer is returned once, when a support code is created.
type SupportOffer struct {
	Support    SupportSession `json:"support"`
	Code       string         `json:"code"`
	ConnectURL string         `json:"connect_url"`
}

// StartSupport issues a support code for one session and publishes its hash.
func (r *HostedRuntime) StartSupport(ctx context.Context, sessionID, title, role string, access time.Duration) (SupportOffer, error) {
	config, err := r.current()
	if err != nil {
		return SupportOffer{}, err
	}
	support, code, err := r.manager.CreateSupportSession(sessionID, title, role, access)
	if err != nil {
		return SupportOffer{}, err
	}
	codeHash, err := r.manager.SupportCodeHash(support.ID)
	if err == nil {
		var connectURL string
		connectURL, err = RegisterHostedSupport(ctx, r.client, config, r.identity, support, codeHash)
		if err == nil {
			return SupportOffer{Support: support, Code: code, ConnectURL: connectURL}, nil
		}
	}
	_, _ = r.manager.EndSupport(support.ID, "bridge_unavailable")
	return SupportOffer{}, err
}

// SupportSessions returns local records, first adopting disconnects the
// supporter made on the bridge so the owner sees an accurate state.
func (r *HostedRuntime) SupportSessions(ctx context.Context) []SupportSession {
	return r.supportSessions(ctx, false)
}

// SyncSupport is SupportSessions with an immediate bridge sync, for one-shot
// callers such as `sessionsift support list`.
func (r *HostedRuntime) SyncSupport(ctx context.Context) []SupportSession {
	return r.supportSessions(ctx, true)
}

func (r *HostedRuntime) supportSessions(ctx context.Context, force bool) []SupportSession {
	if r == nil {
		return []SupportSession{}
	}
	if config, err := r.current(); err == nil && (force || r.dueForSupportSync()) {
		if remote, listErr := ListHostedSupport(ctx, r.client, config, r.identity); listErr == nil {
			for _, value := range remote {
				if value.RevokedAt != nil {
					reason := value.EndReason
					if reason == "" {
						reason = "ended_on_bridge"
					}
					_, _ = r.manager.endSupportIfActive(value.ID, reason)
				}
			}
		}
	}
	return r.manager.SupportSessions()
}

// supportSyncInterval spaces bridge syncs so UI polling cannot consume the
// device's signed-request budget that the relay loop depends on.
const supportSyncInterval = 15 * time.Second

// dueForSupportSync reports whether a live local support session exists and
// the last bridge sync is old enough to repeat.
func (r *HostedRuntime) dueForSupportSync() bool {
	live := false
	now := time.Now()
	for _, value := range r.manager.SupportSessions() {
		if value.Status == "waiting" || value.Status == "connected" {
			live = true
			break
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !live || now.Sub(r.lastSupportSync) < supportSyncInterval {
		return false
	}
	r.lastSupportSync = now
	return true
}

// EndSupport ends access locally first, then asks the bridge to stop routing.
func (r *HostedRuntime) EndSupport(ctx context.Context, id string) (SupportSession, error) {
	support, err := r.manager.EndSupport(id, "ended_by_owner")
	if err != nil {
		return SupportSession{}, err
	}
	if config, configErr := r.current(); configErr == nil {
		if revokeErr := RevokeHostedSupport(ctx, r.client, config, r.identity, id); revokeErr != nil && !strings.Contains(revokeErr.Error(), "not found") {
			return support, revokeErr
		}
	}
	return support, nil
}

func defaultDeviceName() string {
	if hostname, err := os.Hostname(); err == nil && strings.TrimSpace(hostname) != "" {
		return strings.TrimSpace(hostname)
	}
	return "SessionSift device"
}

// DefaultDeviceName is the name used when the owner does not choose one.
func DefaultDeviceName() string { return defaultDeviceName() }
