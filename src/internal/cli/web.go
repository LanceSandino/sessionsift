// Package cli opens only the ready loopback dashboard, never a caller-supplied URL.
// Dependencies: local listener configuration, bounded HTTP readiness, OS browser launcher.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"time"
)

func dashboardURL(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", errors.New("dashboard address must be a loopback host and port")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", errors.New("invalid dashboard port")
	}
	switch host {
	case "localhost", "0.0.0.0":
		host = "127.0.0.1"
	case "127.0.0.1", "::1":
	default:
		return "", errors.New("dashboard browser target must be loopback")
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(number)), nil
}

func web(ctx context.Context, args []string, out, errOut io.Writer) int {
	noBrowser := false
	if len(args) != 0 {
		if len(args) != 1 || args[0] != "--no-browser" {
			fmt.Fprintln(errOut, "usage: sessionsift web [--no-browser]")
			return 2
		}
		noBrowser = true
	}
	url, err := dashboardURL(listenAddress())
	if err != nil {
		return fail(errOut, err)
	}
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(checkCtx, http.MethodGet, url+"/readyz", nil)
	if err != nil {
		return fail(errOut, err)
	}
	// Do not honor proxy variables or follow redirects away from the local service.
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	ready := false
	if err == nil {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 4097))
		response.Body.Close()
		var value struct {
			Status string `json:"status"`
		}
		ready = readErr == nil && len(body) <= 4096 && response.StatusCode == http.StatusOK && json.Unmarshal(body, &value) == nil && value.Status == "ready"
	}
	fmt.Fprintln(out, url)
	if !ready {
		fmt.Fprintln(errOut, "SessionSift is not ready. Start it in another terminal with: sessionsift start\nFor a Homebrew installation: brew services start sessionsift")
		return 1
	}
	if noBrowser {
		return 0
	}
	openCtx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	if err := openDashboardBrowser(openCtx, url); err != nil {
		fmt.Fprintf(errOut, "Could not open the browser: %v. Open the URL above manually, or use sessionsift web --no-browser.\n", err)
		return 1
	}
	return 0
}

var openDashboardBrowser = func(ctx context.Context, url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.CommandContext(ctx, "/usr/bin/open", url).Run()
	case "linux":
		return exec.CommandContext(ctx, "xdg-open", url).Run()
	default:
		return errors.New("automatic browser opening is supported on macOS and Linux only")
	}
}
