// @module BridgeMaintenanceCLI exposes only explicit offline KEK maintenance on an absolute database path.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	bridgeapp "github.com/LanceSandino/sessionsift/src/internal/bridge"
)

func bridgeCommand(ctx context.Context, args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		return startBridge(ctx, out, errOut)
	}
	if args[0] != "kek-rotate" && args[0] != "kek-recover" && args[0] != "kek-preflight" {
		fmt.Fprintln(errOut, "bridge accepts kek-preflight, kek-rotate or kek-recover for offline maintenance")
		return 2
	}
	flags := flag.NewFlagSet("bridge maintenance", flag.ContinueOnError)
	flags.SetOutput(errOut)
	database := flags.String("database", "", "absolute bridge database path; stop all writers first")
	var value *string
	if args[0] == "kek-rotate" {
		value = flags.String("operation", "", "unique non-secret operation ID")
	} else if args[0] == "kek-recover" {
		value = flags.String("action", "", "commit or rollback")
	}
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *database == "" || (value != nil && *value == "") {
		fmt.Fprintln(errOut, "explicit --database and --operation or --action required; no key bytes accepted")
		return 2
	}
	var result bridgeapp.RotationResult
	var err error
	if args[0] == "kek-rotate" {
		result, err = bridgeapp.RotateKEK(ctx, *database, *value)
	} else if args[0] == "kek-recover" {
		result, err = bridgeapp.RecoverKEK(ctx, *database, *value)
	} else {
		result, err = bridgeapp.PreflightKEK(ctx, *database)
	}
	if err != nil {
		fmt.Fprintln(errOut, "bridge maintenance failed:", err)
		return 1
	}
	if err = json.NewEncoder(out).Encode(result); err != nil {
		return 1
	}
	return 0
}
