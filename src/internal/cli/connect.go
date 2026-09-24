// @module CLIConnect implements the bridge connection commands: connect, link, share, revoke,
// status, and code-based support sessions started from the source machine.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/config"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
	"github.com/LanceSandino/sessionsift/src/internal/remote"
)

// linkPollInterval paces `agent link` while it waits for the owner.
var linkPollInterval = 2 * time.Second

const agentUsage = "usage: sessionsift agent <connect|link|share|revoke|status>"

func agentCommand(ctx context.Context, paths config.Paths, args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, agentUsage)
		return 2
	}
	if err := paths.Ensure(); err != nil {
		return fail(errOut, err)
	}
	identity, err := remote.LoadOrCreateIdentity(paths.DeviceIdentity)
	if err != nil {
		return fail(errOut, err)
	}
	manager, err := remote.OpenManager(paths.RemoteState)
	if err != nil {
		return fail(errOut, err)
	}
	hosted, err := remote.OpenHostedClient(paths.HostedBridge, identity, manager, hostedHTTPClient)
	if err != nil {
		return fail(errOut, err)
	}
	switch args[0] {
	case "connect":
		if len(args) < 2 {
			fmt.Fprintln(errOut, "usage: sessionsift agent connect <bridge-url> [device-name]")
			return 2
		}
		name := ""
		if len(args) > 2 {
			name = strings.Join(args[2:], " ")
		}
		status, connectErr := hosted.Connect(ctx, args[1], name)
		if connectErr != nil {
			return fail(errOut, connectErr)
		}
		fmt.Fprintf(out, "Connected %s (%s) to %s.\n", status.Name, status.DeviceID, status.URL)
		if status.Owner == "" {
			fmt.Fprintln(out, "Share a session with `sessionsift support start <session-id>`, or link this device to your account with `sessionsift agent link`.")
		}
		fmt.Fprintln(out, "Keep SessionSift running (sessionsift start) so connections can reach this device.")
		return 0
	case "enroll":
		fmt.Fprintln(errOut, "`agent enroll` was replaced: run `sessionsift agent connect <bridge-url>`, then `sessionsift agent link` and enter the code it shows in the bridge console.")
		return 2
	case "link":
		return linkCommand(ctx, hosted, args[1:], out, errOut)
	case "share":
		if len(args) < 2 {
			fmt.Fprintln(errOut, "usage: sessionsift agent share <email> [days]")
			return 2
		}
		duration := 30 * 24 * time.Hour
		if len(args) > 2 {
			days, parseErr := strconv.Atoi(args[2])
			if parseErr != nil || days < 1 || days > 30 {
				fmt.Fprintln(errOut, "days must be between 1 and 30")
				return 2
			}
			duration = time.Duration(days) * 24 * time.Hour
		}
		hostedConfig, loadErr := remote.LoadHostedConfig(paths.HostedBridge)
		if loadErr != nil {
			return fail(errOut, errors.New("this device is not connected; run sessionsift agent connect first"))
		}
		grant, grantErr := manager.ApproveOAuthPrincipal(args[1], protocol.ReadOperations(), duration)
		if grantErr != nil {
			return fail(errOut, grantErr)
		}
		if grantErr = remote.RegisterHostedGrant(ctx, hostedHTTPClient, hostedConfig, identity, grant.Principal, grant.Operations, grant.ExpiresAt); grantErr != nil {
			_ = manager.Revoke(grant.ID)
			return fail(errOut, grantErr)
		}
		fmt.Fprintf(out, "Shared read-only session access with %s until %s.\n", grant.Principal, grant.ExpiresAt.Format(time.RFC3339))
		return 0
	case "revoke":
		if len(args) < 2 {
			fmt.Fprintln(errOut, "usage: sessionsift agent revoke <email>")
			return 2
		}
		hostedConfig, loadErr := remote.LoadHostedConfig(paths.HostedBridge)
		if loadErr != nil {
			return fail(errOut, loadErr)
		}
		if revokeErr := remote.RevokeHostedGrant(ctx, hostedHTTPClient, hostedConfig, identity, args[1]); revokeErr != nil {
			return fail(errOut, revokeErr)
		}
		if revokeErr := manager.RevokePrincipal(args[1]); revokeErr != nil {
			return fail(errOut, revokeErr)
		}
		fmt.Fprintf(out, "Revoked hosted and local access for %s.\n", strings.ToLower(strings.TrimSpace(args[1])))
		return 0
	case "status":
		status := hosted.Status()
		if !status.Connected {
			fmt.Fprintln(out, "Hosted bridge: not connected")
			return 0
		}
		if refreshed, refreshErr := hosted.RefreshOwner(ctx); refreshErr == nil {
			status = refreshed
		} else {
			fmt.Fprintf(errOut, "bridge status warning: %v\n", refreshErr)
		}
		owner := status.Owner
		if owner == "" {
			owner = "not linked (run sessionsift agent link)"
		}
		fmt.Fprintf(out, "Hosted bridge: %s\nDevice: %s\nOwner: %s\nLocal OAuth grants: %d\n", status.URL, status.DeviceID, owner, len(manager.OAuthGrants()))
		return 0
	default:
		fmt.Fprintln(errOut, agentUsage)
		return 2
	}
}

// linkCommand shows a link code and, unless --no-wait is given, waits until an
// owner enters it in the bridge console.
func linkCommand(ctx context.Context, hosted *remote.HostedRuntime, args []string, out, errOut io.Writer) int {
	code, expires, err := hosted.StartLink(ctx)
	if err != nil {
		return fail(errOut, err)
	}
	status := hosted.Status()
	fmt.Fprintf(out, "Link code: %s\nEnter it at %s/ under \"Add a device\" before %s.\n", code, status.URL, expires.Local().Format("15:04"))
	if len(args) > 0 && args[0] == "--no-wait" {
		return 0
	}
	fmt.Fprintln(out, "Waiting for the owner to enter the code…")
	for time.Now().Before(expires) {
		select {
		case <-ctx.Done():
			return fail(errOut, ctx.Err())
		case <-time.After(linkPollInterval):
		}
		refreshed, refreshErr := hosted.RefreshOwner(ctx)
		if refreshErr != nil {
			fmt.Fprintf(errOut, "link status warning: %v\n", refreshErr)
			continue
		}
		if refreshed.Owner != "" {
			fmt.Fprintf(out, "Linked to %s.\n", refreshed.Owner)
			return 0
		}
	}
	return fail(errOut, errors.New("the link code expired before it was entered; run sessionsift agent link again"))
}

const supportUsage = "usage: sessionsift support <start <session-id> [view|comment|review] [minutes]|list|end <support-id>>"

// supportCommand lets the source machine hand out a support code from a shell.
func supportCommand(ctx context.Context, paths config.Paths, args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, supportUsage)
		return 2
	}
	store, err := openStore(paths)
	if err != nil {
		return fail(errOut, err)
	}
	defer store.Close()
	identity, err := remote.LoadOrCreateIdentity(paths.DeviceIdentity)
	if err != nil {
		return fail(errOut, err)
	}
	manager, err := remote.OpenManager(paths.RemoteState)
	if err != nil {
		return fail(errOut, err)
	}
	hosted, err := remote.OpenHostedClient(paths.HostedBridge, identity, manager, hostedHTTPClient)
	if err != nil {
		return fail(errOut, err)
	}
	switch args[0] {
	case "start":
		if len(args) < 2 {
			fmt.Fprintln(errOut, supportUsage)
			return 2
		}
		role, minutes := "view", 60
		if len(args) > 2 {
			role = args[2]
		}
		if len(args) > 3 {
			if minutes, err = strconv.Atoi(args[3]); err != nil {
				fmt.Fprintln(errOut, "minutes must be a number between 5 and 480")
				return 2
			}
		}
		session, getErr := store.GetSession(ctx, args[1])
		if getErr != nil {
			return fail(errOut, errors.New("session not found; list sessions with sessionsift sessions"))
		}
		offer, startErr := hosted.StartSupport(ctx, session.ID, session.Title, role, time.Duration(minutes)*time.Minute)
		if startErr != nil {
			return fail(errOut, startErr)
		}
		fmt.Fprintf(out, "Support code: %s\nThe other person enters it at %s before %s.\nAccess: %s for %d minutes to \"%s\" only.\nEnd it any time with: sessionsift support end %s\n",
			offer.Code, offer.ConnectURL, offer.Support.CodeExpiresAt.Local().Format("15:04"), offer.Support.Role, offer.Support.AccessMinutes, offer.Support.SessionTitle, offer.Support.ID)
		if _, healthErr := getHealth(); healthErr != nil {
			fmt.Fprintln(errOut, "SessionSift is not running here; start it (sessionsift start) or the code cannot connect.")
		}
		return 0
	case "list":
		for _, value := range hosted.SyncSupport(ctx) {
			who := value.Supporter
			if who == "" {
				who = "-"
			}
			fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%s\n", value.ID, value.Status, value.Role, who, value.SessionTitle)
		}
		return 0
	case "end":
		if len(args) < 2 {
			fmt.Fprintln(errOut, supportUsage)
			return 2
		}
		support, endErr := hosted.EndSupport(ctx, args[1])
		if endErr != nil && support.ID == "" {
			return fail(errOut, endErr)
		}
		if endErr != nil {
			fmt.Fprintf(errOut, "ended locally; bridge warning: %v\n", endErr)
		}
		fmt.Fprintf(out, "Ended %s.\n", support.ID)
		return 0
	default:
		fmt.Fprintln(errOut, supportUsage)
		return 2
	}
}
