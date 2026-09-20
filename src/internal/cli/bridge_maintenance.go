// @module BridgeMaintenance schedules private paired snapshots and orphan retention for a single-origin beta.
package cli

import (
	"context"
	"fmt"
	bridgeapp "github.com/LanceSandino/sessionsift/src/internal/bridge"
	"io"
	"time"
)

func runBridgeMaintenance(ctx context.Context, store *bridgeapp.Store, directory string, errOut io.Writer) {
	backup := func() {
		if _, err := store.Backup(ctx, directory); err != nil && ctx.Err() == nil {
			fmt.Fprintln(errOut, "bridge backup failed; check protected storage and permissions")
		}
	}
	backup()
	daily := time.NewTicker(24 * time.Hour)
	defer daily.Stop()
	retention := time.NewTicker(5 * time.Minute)
	defer retention.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-daily.C:
			backup()
		case <-retention.C:
			if err := store.Maintain(ctx); err != nil && ctx.Err() == nil {
				fmt.Fprintln(errOut, "bridge retention failed; check protected storage")
			}
		}
	}
}
