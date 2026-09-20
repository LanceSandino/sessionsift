package remote_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/bridge"
	"github.com/LanceSandino/sessionsift/src/internal/domain"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
	"github.com/LanceSandino/sessionsift/src/internal/remote"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

func TestHostedAgentEnrollsAndServesPrincipalScopedTasks(t *testing.T) {
	state := t.TempDir()
	bridgeStore, err := bridge.Open(filepath.Join(state, "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bridgeStore.Close()
	handler := bridge.NewHandler(bridgeStore, nil, "")
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Result(), nil
	})}

	identity, err := remote.LoadOrCreateIdentity(filepath.Join(state, "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	enrollment, _ := bridgeStore.CreateEnrollment("owner@example.com")
	config, err := remote.EnrollHosted(context.Background(), client, "http://localhost", enrollment.Code, "Test laptop", identity)
	if err != nil || config.DeviceID != identity.ID {
		t.Fatalf("config=%+v err=%v", config, err)
	}

	manager, _ := remote.OpenManager(filepath.Join(state, "remote.json"))
	grant, err := manager.ApproveOAuthPrincipal(config.Owner, []protocol.Operation{protocol.SessionList}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err = remote.RegisterHostedGrant(context.Background(), client, config, identity, config.Owner, grant.Operations, grant.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	repository, _ := storage.Open(filepath.Join(state, "local.db"))
	defer repository.Close()
	now := time.Now().UTC()
	if err = repository.SaveImport(context.Background(), "path", "fingerprint", domain.Session{ID: "s1", ExternalSessionID: "s1", Title: "Private session", StartedAt: now, EndedAt: now, ParserVersion: domain.ParserVersion}); err != nil {
		t.Fatal(err)
	}
	request := protocol.Request{Version: 1, RequestID: "hosted-1", Operation: protocol.SessionList, Arguments: json.RawMessage(`{}`)}
	task, err := bridgeStore.Submit(config.Owner, config.DeviceID, request)
	if err != nil {
		t.Fatal(err)
	}
	agent := remote.NewAgent(identity, manager, repository)
	worked, err := agent.ServeHostedNext(context.Background(), client, config)
	if err != nil || !worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	response, err := bridgeStore.Wait(context.Background(), task.ID, time.Second)
	if err != nil || response.Status != protocol.StatusOK {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	loopCtx, cancel := context.WithCancel(context.Background())
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		remote.RunHostedLoop(loopCtx, agent, client, config, func(error) {})
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-loopDone
	if err = remote.RevokeHostedGrant(context.Background(), client, config, identity, config.Owner); err != nil {
		t.Fatal(err)
	}
	if _, err = bridgeStore.Submit(config.Owner, config.DeviceID, request); err == nil {
		t.Fatal("revoked hosted grant remained active")
	}
}

func TestHostedInvitationCreatesLocalScopeAndServesCollaboration(t *testing.T) {
	state := t.TempDir()
	bridgeStore, _ := bridge.Open(filepath.Join(state, "bridge.db"))
	defer bridgeStore.Close()
	handler := bridge.NewHandler(bridgeStore, nil, "http://localhost")
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Result(), nil
	})}
	identity, _ := remote.LoadOrCreateIdentity(filepath.Join(state, "identity.json"))
	enrollment, _ := bridgeStore.CreateEnrollment("owner@example.com")
	config, err := remote.EnrollHosted(context.Background(), client, "http://localhost", enrollment.Code, "Test laptop", identity)
	if err != nil {
		t.Fatal(err)
	}
	manager, _ := remote.OpenManager(filepath.Join(state, "remote.json"))
	inviter := remote.NewHostedInviter(client, config, identity, manager)
	invitation, err := inviter.Create(context.Background(), "reviewer@example.com", "s1", "comment", time.Hour)
	if err != nil || invitation.AcceptURL == "" || len(manager.InviteGrants()) != 1 {
		t.Fatalf("invitation=%+v grants=%+v err=%v", invitation, manager.InviteGrants(), err)
	}
	listed, err := inviter.List(context.Background())
	if err != nil || len(listed) != 1 || listed[0].ID != invitation.ID {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
	code := invitation.AcceptURL[strings.LastIndex(invitation.AcceptURL, "/")+1:]
	_, token, err := bridgeStore.AcceptInvitation(code, invitation.Invitee)
	if err != nil {
		t.Fatal(err)
	}
	repository, _ := storage.Open(filepath.Join(state, "local.db"))
	defer repository.Close()
	now := time.Now().UTC()
	if err = repository.SaveImport(context.Background(), "path", "fingerprint", domain.Session{ID: "s1", ExternalSessionID: "s1", Title: "Shared session", StartedAt: now, EndedAt: now, ParserVersion: domain.ParserVersion}); err != nil {
		t.Fatal(err)
	}
	request := protocol.Request{Version: 1, RequestID: "comment-1", Operation: protocol.AnnotationCreate, Arguments: json.RawMessage(`{"session_id":"s1","body":"Looks good"}`)}
	task, err := bridgeStore.SubmitInvitation(invitation.Invitee, invitation.ID, token, request)
	if err != nil {
		t.Fatal(err)
	}
	agent := remote.NewAgent(identity, manager, repository)
	if worked, serveErr := agent.ServeHostedNext(context.Background(), client, config); serveErr != nil || !worked {
		t.Fatalf("worked=%v err=%v", worked, serveErr)
	}
	response, err := bridgeStore.Wait(context.Background(), task.ID, time.Second)
	if err != nil || response.Status != protocol.StatusOK || len(manager.Annotations("s1")) != 1 {
		t.Fatalf("response=%+v annotations=%+v err=%v", response, manager.Annotations("s1"), err)
	}
	if err = inviter.Revoke(context.Background(), invitation.ID); err != nil {
		t.Fatal(err)
	}
	if err = manager.AuthorizePrincipalRequest(context.Background(), invitation.Invitee, protocol.Request{Version: 1, RequestID: "revoked", Operation: protocol.SessionGet, Arguments: json.RawMessage(`{"session_id":"s1"}`)}); err == nil {
		t.Fatal("revoked local invitation remained active")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestHostedConfigIsPrivateAndRejectsPublicHTTP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridge.json")
	if err := remote.SaveHostedConfig(path, remote.HostedConfig{URL: "http://public.example", DeviceID: "d", Owner: "o@example.com"}); err == nil {
		t.Fatal("public HTTP bridge URL was accepted")
	}
	if err := remote.SaveHostedConfig(path, remote.HostedConfig{URL: "https://sessionsift.example", DeviceID: "d", Owner: "o@example.com"}); err != nil {
		t.Fatal(err)
	}
	loaded, err := remote.LoadHostedConfig(path)
	if err != nil || loaded.URL != "https://sessionsift.example" {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	if _, err = remote.LoadHostedConfig(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("missing hosted config loaded")
	}
	broken := filepath.Join(t.TempDir(), "broken.json")
	if err = os.WriteFile(broken, []byte(`{"url":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = remote.LoadHostedConfig(broken); err == nil {
		t.Fatal("invalid hosted config loaded")
	}
}

func TestHostedClientReportsBridgeRefusals(t *testing.T) {
	identity, _ := remote.LoadOrCreateIdentity(filepath.Join(t.TempDir(), "identity.json"))
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadRequest, Status: "400 Bad Request", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"refused"}`))}, nil
	})}
	if _, err := remote.EnrollHosted(context.Background(), client, "http://localhost", "code", "Laptop", identity); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("enrollment error=%v", err)
	}
	config := remote.HostedConfig{URL: "http://localhost", DeviceID: identity.ID, Owner: "owner@example.com"}
	if err := remote.RegisterHostedGrant(context.Background(), client, config, identity, config.Owner, protocol.ReadOperations(), time.Now().Add(time.Hour)); err == nil {
		t.Fatal("grant refusal was ignored")
	}
	if _, err := remote.NewAgent(identity, mustManager(t), nil).ServeHostedNext(context.Background(), client, config); err == nil {
		t.Fatal("poll refusal was ignored")
	}
}

func mustManager(t *testing.T) *remote.Manager {
	t.Helper()
	manager, err := remote.OpenManager(filepath.Join(t.TempDir(), "remote.json"))
	if err != nil {
		t.Fatal(err)
	}
	return manager
}
