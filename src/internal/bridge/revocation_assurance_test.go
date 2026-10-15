// @module InFlightGrantAssurance validates grant expiry/revocation at response release (A06).
package bridge

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
)

func TestA06InFlightGrantRevocationAndExpiry(t *testing.T) {
	for _, attack := range []string{"revoked", "expired", "valid"} {
		t.Run(attack, func(t *testing.T) {
			store, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			public, private, _ := ed25519.GenerateKey(rand.Reader)
			device := testOwnedDevice(t, store, "owner@example.com", "synthetic", public)
			if _, err = store.RegisterGrant(device.ID, "owner@example.com", []protocol.Operation{protocol.SessionList}, time.Now().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			request := protocol.Request{Version: 1, RequestID: "inflight", Operation: protocol.SessionList, Arguments: json.RawMessage(`{}`)}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			finished := make(chan protocol.Response, 1)
			go func() {
				finished <- (relayExecutor{store: store, principal: "owner@example.com", deviceID: device.ID}).Execute(ctx, request)
			}()
			var task Task
			found := false
			for i := 0; i < 100; i++ {
				task, found, err = store.Next(device.ID)
				if err != nil {
					t.Fatal(err)
				}
				if found {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if !found {
				t.Fatal("fixture task was not claimed")
			}
			switch attack {
			case "revoked":
				err = store.RevokeGrant(device.ID, "owner@example.com")
			case "expired":
				_, err = store.db.Exec(`UPDATE grants SET expires_at=? WHERE device_id=?`, time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano), device.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			response, _ := json.Marshal(protocol.Response{Version: 1, RequestID: request.RequestID, Status: protocol.StatusOK, Data: map[string]string{"synthetic": "PRIVATE-RESPONSE"}})
			if err = store.Complete(device.ID, task.ID, response, ed25519.Sign(private, response)); err != nil {
				t.Fatal(err)
			}
			result := <-finished
			if attack == "valid" {
				if result.Status != protocol.StatusOK || result.Data == nil {
					t.Fatal("positive control denied")
				}
			} else if result.Status == protocol.StatusOK || result.Data != nil {
				t.Fatalf("in-flight %s released response: %+v", attack, result)
			}
		})
	}
}
