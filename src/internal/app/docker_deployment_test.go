// Package app tests the checked-in Docker service contract.
// @module DockerDeploymentTests anchors private mounts, health checks and restart policy.
package app

import (
	"os"
	"strings"
	"testing"
)

func TestDockerServiceContract(t *testing.T) {
	localCompose, err := os.ReadFile("../../../compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	remoteCompose, err := os.ReadFile("../../../compose.remote.yaml")
	if err != nil {
		t.Fatal(err)
	}
	localText := string(localCompose)
	remoteText := string(remoteCompose)
	text := localText + "\n" + remoteText
	for _, required := range []string{
		"agent:", "bridge:", "restart: unless-stopped", "127.0.0.1:7777:7777", "127.0.0.1:7780:7780",
		".sessionsift:/state", ".sessionsift-bridge:/bridge-state", ".codex/sessions:/sources/codex/sessions:ro",
		"read_only: true", "no-new-privileges:true", "healthcheck:",
		"SESSIONSIFT_TRUSTED_PROXY_IP: 172.30.77.1", "SESSIONSIFT_TRUSTED_PROXY_IP: 172.30.78.1",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("compose.yaml missing %q", required)
		}
	}
	if strings.Contains(localText, "bridge:") || strings.Contains(localText, "SESSIONSIFT_ACCESS_ISSUER") {
		t.Fatal("the default Compose service must remain local-only")
	}
	if !strings.Contains(remoteText, "bridge:") || !strings.Contains(remoteText, "SESSIONSIFT_ACCESS_ISSUER") {
		t.Fatal("the optional remote Compose file must contain the hosted bridge")
	}
	if strings.Contains(text, ".codex:/sources/codex") || strings.Contains(text, ".env.cloudflare") {
		t.Fatal("compose must not mount the whole Codex home or load the curl credential file")
	}
	if strings.Count(text, "gateway:") != 2 {
		t.Fatal("each isolated service network must declare its exact trusted gateway")
	}
	dockerfile, err := os.ReadFile("../../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dockerfile), "USER nonroot:nonroot") {
		t.Fatal("runtime image must declare a non-root user")
	}
}
