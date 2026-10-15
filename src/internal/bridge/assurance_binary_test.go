//go:build assurance_binary

// @module BinarySyncAssurance exercises the actual candidate process with isolated enrolled devices (A01/A02/A06/A10).
package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/cloudsync"
	"github.com/LanceSandino/sessionsift/src/internal/remote"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

func TestA10BuiltBinaryMultiDeviceRecovery(t *testing.T) {
	binary := os.Getenv("SESSIONSIFT_ASSURANCE_BINARY")
	if binary == "" {
		t.Fatal("built binary fixture required")
	}
	root := t.TempDir()
	path := filepath.Join(root, "bridge.db")
	serverStore, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := remote.LoadOrCreateIdentity(filepath.Join(root, "a.json"))
	b, _ := remote.LoadOrCreateIdentity(filepath.Join(root, "b.json"))
	other, _ := remote.LoadOrCreateIdentity(filepath.Join(root, "other.json"))
	deviceA := testOwnedDevice(t, serverStore, "owner@example.com", "A", a.PublicKey)
	deviceB := testOwnedDeviceIn(t, serverStore, "owner@example.com", deviceA.WorkspaceID, "B", b.PublicKey)
	deviceOther := testOwnedDevice(t, serverStore, "other@example.com", "Other", other.PublicKey)
	if err = serverStore.Close(); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	url := "http://" + address
	var logs bytes.Buffer
	var cmd *exec.Cmd
	stop := func() {
		if cmd != nil {
			cmd.Process.Kill()
			cmd.Wait()
			cmd = nil
		}
	}
	defer stop()
	start := func() {
		cmd = exec.Command(binary, "bridge")
		cmd.Env = append(os.Environ(), "SESSIONSIFT_BRIDGE_HOME="+root, "SESSIONSIFT_BRIDGE_ADDR="+address, "SESSIONSIFT_SYNC_ENABLED=true", "SESSIONSIFT_ACCESS_ISSUER=https://identity.example.invalid", "SESSIONSIFT_ACCESS_AUD=synthetic", "SESSIONSIFT_PUBLIC_URL=https://bridge.example.invalid")
		cmd.Stdout = &logs
		cmd.Stderr = &logs
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		client := http.Client{Timeout: time.Second}
		for i := 0; i < 100; i++ {
			response, err := client.Get(url + "/readyz")
			if err == nil {
				response.Body.Close()
				if response.StatusCode == 200 {
					return
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		stop()
		t.Fatal("candidate did not become ready")
	}
	master, _ := cloudsync.GenerateMasterKey(nil)
	localA, err := storage.Open(filepath.Join(root, "local-a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer localA.Close()
	localB, err := storage.Open(filepath.Join(root, "local-b.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer localB.Close()
	client := &http.Client{Timeout: 2 * time.Second}
	transportA := &cloudsync.HTTPTransport{Client: client, Config: remote.HostedConfig{URL: url, DeviceID: deviceA.ID, WorkspaceID: deviceA.WorkspaceID}, Identity: a}
	transportB := &cloudsync.HTTPTransport{Client: client, Config: remote.HostedConfig{URL: url, DeviceID: deviceB.ID, WorkspaceID: deviceA.WorkspaceID}, Identity: b}
	now := time.Now().Add(time.Hour)
	engineA := cloudsync.Engine{Store: localA, Transport: transportA, MasterKey: master, WorkspaceID: deviceA.WorkspaceID, DeviceID: deviceA.ID, Epoch: 1, Sign: a.Sign, Now: func() time.Time { return now }}
	payload := json.RawMessage(`{"title":"M8-BINARY-CANARY","note":"synthetic"}`)
	if err = engineA.Queue(context.Background(), cloudsync.Record{LocalID: "M8-PRIVATE-ID", Kind: "note", Revision: 1, Payload: payload}, 0); err != nil {
		t.Fatal(err)
	}
	if err = engineA.RunOnce(context.Background()); err == nil {
		t.Fatal("offline upload passed")
	}
	start()
	now = now.Add(time.Hour)
	if err = engineA.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	head, err := transportA.Get(context.Background(), cloudsync.ObjectID(master, "note", "M8-PRIVATE-ID"))
	if err != nil {
		t.Fatal(err)
	}
	payload = json.RawMessage(`{"title":"M8-BINARY-CANARY","note":"latest offline revision"}`)
	if err = engineA.Queue(context.Background(), cloudsync.Record{LocalID: "M8-PRIVATE-ID", Kind: "note", Revision: 2, ParentDigest: head.Digest, Payload: payload}, 1); err != nil {
		t.Fatal(err)
	}
	if err = engineA.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	stop()
	start() // durable ciphertext and enrollment survive actual process loss
	engineB := cloudsync.Engine{Store: localB, Transport: transportB, MasterKey: master, WorkspaceID: deviceA.WorkspaceID, DeviceID: deviceB.ID, Epoch: 1, Sign: b.Sign}
	if err = engineB.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	replicas, err := localB.ListSyncReplicas(context.Background())
	if err != nil || len(replicas) != 1 || !bytes.Equal(replicas[0].PayloadJSON, payload) {
		t.Fatal("replacement device failed restore")
	}
	head, err = transportA.Get(context.Background(), replicas[0].ObjectID)
	if err != nil {
		t.Fatal(err)
	}
	if err = engineA.Queue(context.Background(), cloudsync.Record{LocalID: "M8-PRIVATE-ID", Kind: "note", Revision: 3, ParentDigest: head.Digest, Payload: json.RawMessage(`{}`), Deleted: true}, 2); err != nil {
		t.Fatal(err)
	}
	if err = engineA.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = engineB.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	deleted, err := localB.ListSyncReplicas(context.Background())
	if err != nil || len(deleted) != 1 || !deleted[0].Deleted || deleted[0].Revision != 3 {
		t.Fatal("authenticated tombstone did not apply")
	}
	foreign := &cloudsync.HTTPTransport{Client: client, Config: remote.HostedConfig{URL: url, DeviceID: deviceOther.ID, WorkspaceID: deviceOther.WorkspaceID}, Identity: other}
	if _, err = foreign.Get(context.Background(), replicas[0].ObjectID); err == nil {
		t.Fatal("cross-tenant read passed")
	}
	stop()
	serverStore, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = serverStore.RevokeDevice(deviceB.ID, "owner@example.com"); err != nil {
		t.Fatal(err)
	}
	serverStore.Close()
	start()
	if _, _, err = transportB.Changes(context.Background(), 0, 100); err == nil {
		t.Fatal("revoked device survived restart")
	}
	stop()
	for _, name := range []string{"bridge.db", "bridge.db-wal"} {
		body, err := os.ReadFile(filepath.Join(root, name))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if bytes.Contains(body, []byte("M8-BINARY-CANARY")) || bytes.Contains(body, []byte("M8-PRIVATE-ID")) || bytes.Contains(body, []byte(cloudsync.EncodeRecoverySecret(master))) {
			t.Fatal("server plaintext leak")
		}
	}
	if bytes.Contains(logs.Bytes(), []byte("M8-BINARY-CANARY")) || bytes.Contains(logs.Bytes(), []byte(cloudsync.EncodeRecoverySecret(master))) {
		t.Fatal("log leak")
	}
	t.Log("built candidate: offline outbox, restart, enrolled recovery, tenant denial, durable revocation and ciphertext/log confidentiality passed")
}
