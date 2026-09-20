// Package remote tests outbound-only relay execution through the Local Agent.
package remote

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
)

type repositoryStub struct{ sessions []domain.Session }

func (r repositoryStub) ListSessions(context.Context) ([]domain.Session, error) {
	return r.sessions, nil
}

func TestRelayExecutorSuccessErrorsAndEmptyQueue(t *testing.T) {
	directory := t.TempDir()
	identity, _ := LoadOrCreateIdentity(filepath.Join(directory, "identity.json"))
	manager, _ := OpenManager(filepath.Join(directory, "state.json"))
	pairing, _ := manager.CreatePairing(identity.ID)
	_, _ = manager.ClaimPairing(pairing.Code, "reviewer", []protocol.Operation{protocol.SessionList})
	_, token, _ := manager.ApprovePairing(pairing.Code, time.Hour)
	relay := NewRelay()
	relay.Register(identity.ID, identity.PublicKey)
	agent := NewAgent(identity, manager, repositoryStub{})
	executor := RelayExecutor{Relay: relay, DeviceID: identity.ID, GrantToken: token, Timeout: time.Second}
	done := make(chan struct{})
	go func() {
		for {
			if worked, _ := agent.ServeNext(context.Background(), relay); worked {
				close(done)
				return
			}
		}
	}()
	response := executor.Execute(context.Background(), protocol.Request{Version: 1, RequestID: "executor", Operation: protocol.SessionList, Arguments: json.RawMessage(`{}`)})
	<-done
	if response.Status != protocol.StatusOK {
		t.Fatalf("response=%+v", response)
	}
	bad := (RelayExecutor{Relay: relay, DeviceID: "missing", GrantToken: token}).Execute(context.Background(), protocol.Request{Version: 1, RequestID: "bad", Operation: protocol.SessionList})
	if bad.Status != protocol.StatusError {
		t.Fatal("unknown device accepted")
	}
	if worked, err := agent.ServeNext(context.Background(), relay); worked || err != nil {
		t.Fatalf("empty queue worked=%v err=%v", worked, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := relay.Wait(ctx, "missing"); err == nil {
		t.Fatal("missing wait accepted")
	}
}
func (r repositoryStub) GetSession(_ context.Context, id string) (domain.Session, error) {
	for _, session := range r.sessions {
		if session.ID == id {
			return session, nil
		}
	}
	return domain.Session{}, context.Canceled
}

func TestRelayExecutesAllowlistedRequestAndVerifiesDeviceSignature(t *testing.T) {
	directory := t.TempDir()
	identity, err := LoadOrCreateIdentity(filepath.Join(directory, "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := OpenManager(filepath.Join(directory, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := manager.CreatePairing(identity.ID)
	_, _ = manager.ClaimPairing(pairing.Code, "reviewer", []protocol.Operation{protocol.SessionList})
	_, token, err := manager.ApprovePairing(pairing.Code, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	relay := NewRelay()
	relay.Register(identity.ID, identity.PublicKey)
	agent := NewAgent(identity, manager, repositoryStub{sessions: []domain.Session{{ID: "s1", Title: "Redacted session", StartedAt: time.Now()}}})
	request := protocol.Request{Version: protocol.Version, RequestID: "remote-1", Operation: protocol.SessionList, Arguments: json.RawMessage(`{}`)}
	taskID, err := relay.Submit(identity.ID, token, request)
	if err != nil {
		t.Fatal(err)
	}
	if worked, serveErr := agent.ServeNext(context.Background(), relay); serveErr != nil || !worked {
		t.Fatalf("worked=%v err=%v", worked, serveErr)
	}
	response, err := relay.Wait(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != protocol.StatusOK {
		t.Fatalf("response=%+v", response)
	}
	if len(manager.Audit()) != 1 || !manager.Audit()[0].Allowed {
		t.Fatal("allowed request was not audited")
	}
}

func TestRelayDeniesCapabilityAndRejectsForgedResponse(t *testing.T) {
	directory := t.TempDir()
	identity, _ := LoadOrCreateIdentity(filepath.Join(directory, "identity.json"))
	manager, _ := OpenManager(filepath.Join(directory, "state.json"))
	pairing, _ := manager.CreatePairing(identity.ID)
	_, _ = manager.ClaimPairing(pairing.Code, "reviewer", []protocol.Operation{protocol.SessionList})
	grant, token, _ := manager.ApprovePairing(pairing.Code, time.Hour)
	relay := NewRelay()
	relay.Register(identity.ID, identity.PublicKey)
	agent := NewAgent(identity, manager, repositoryStub{})
	request := protocol.Request{Version: protocol.Version, RequestID: "remote-2", Operation: protocol.SessionGet, Arguments: json.RawMessage(`{"session_id":"s1"}`)}
	taskID, _ := relay.Submit(identity.ID, token, request)
	worked, err := agent.ServeNext(context.Background(), relay)
	if err != nil || !worked {
		t.Fatal(err)
	}
	response, err := relay.Wait(context.Background(), taskID)
	if err != nil || response.Status != protocol.StatusDenied {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	if manager.Audit()[0].GrantID != grant.ID || manager.Audit()[0].Allowed {
		t.Fatalf("audit=%+v", manager.Audit()[0])
	}
	forgedID, _ := relay.Submit(identity.ID, token, request)
	if err = relay.Complete(forgedID, protocol.Response{Status: protocol.StatusOK}, []byte("forged")); err == nil {
		t.Fatal("forged response accepted")
	}
}

func TestHTTPBridgeCarriesSignedOutboundAgentTraffic(t *testing.T) {
	directory := t.TempDir()
	identity, _ := LoadOrCreateIdentity(filepath.Join(directory, "identity.json"))
	manager, _ := OpenManager(filepath.Join(directory, "state.json"))
	pairing, _ := manager.CreatePairing(identity.ID)
	_, _ = manager.ClaimPairing(pairing.Code, "remote-reviewer", []protocol.Operation{protocol.SessionList})
	_, token, _ := manager.ApprovePairing(pairing.Code, time.Hour)
	relay := NewRelay()
	relay.Register(identity.ID, identity.PublicKey)
	handler := NewBridgeHandler(relay)
	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Result(), nil
	})}
	agent := NewAgent(identity, manager, repositoryStub{sessions: []domain.Session{{ID: "s1", Title: "Remote session"}}})
	worked := make(chan error, 1)
	go func() {
		for {
			didWork, err := agent.ServeHTTPNext(context.Background(), client, "https://bridge.example")
			if err != nil || didWork {
				worked <- err
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	executor := HTTPRelayExecutor{Client: client, BridgeURL: "https://bridge.example", DeviceID: identity.ID, GrantToken: token}
	response := executor.Execute(context.Background(), protocol.Request{Version: 1, RequestID: "http-bridge", Operation: protocol.SessionList, Arguments: json.RawMessage(`{}`)})
	if err := <-worked; err != nil {
		t.Fatal(err)
	}
	if response.Status != protocol.StatusOK {
		t.Fatalf("response=%+v", response)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/device/next", nil)
	recorder := httptest.NewRecorder()
	NewBridgeHandler(relay).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned poll status=%d", recorder.Code)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Body == nil {
		request.Body = io.NopCloser(strings.NewReader(""))
	}
	return f(request)
}

func TestRelayResponseMustComeFromAssignedDevice(t *testing.T) {
	first, _ := LoadOrCreateIdentity(filepath.Join(t.TempDir(), "first.json"))
	second, _ := LoadOrCreateIdentity(filepath.Join(t.TempDir(), "second.json"))
	relay := NewRelay()
	relay.Register(first.ID, first.PublicKey)
	relay.Register(second.ID, second.PublicKey)
	id, _ := relay.Submit(first.ID, "grant", protocol.Request{Version: 1, RequestID: "bound", Operation: protocol.SessionList})
	response := protocol.Response{Version: 1, RequestID: "bound", Status: protocol.StatusOK}
	encoded, _ := json.Marshal(response)
	if err := relay.Complete(id, response, second.Sign(encoded)); err == nil {
		t.Fatal("another registered device completed the task")
	}
}
