// Package bridge provides shared fixtures for bridge tests.
package bridge

import (
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/supportcode"
)

// testOwnedDevice registers a device key and links it to owner through the
// production flow: the device shows a link code and the owner enters it.
func testOwnedDevice(t *testing.T, store *Store, owner, name string, public ed25519.PublicKey) Device {
	t.Helper()
	return testOwnedDeviceIn(t, store, owner, "", name, public)
}

func testOwnedDeviceIn(t *testing.T, store *Store, owner, workspaceID, name string, public ed25519.PublicKey) Device {
	t.Helper()
	device, err := store.RegisterDevice(name, public)
	if err != nil {
		t.Fatal(err)
	}
	linked, err := store.ClaimDeviceLink(owner, testLinkCode(t, store, device.ID), workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	return linked
}

// testLinkCode issues a link code the way a Local Agent does.
func testLinkCode(t *testing.T, store *Store, deviceID string) string {
	t.Helper()
	code, err := supportcode.Generate()
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := supportcode.Hash(supportcode.Link, code)
	if err = store.CreateDeviceLink(deviceID, hash, time.Now().Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	return code
}
