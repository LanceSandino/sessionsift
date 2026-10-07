// @module CollectionWatch paces background transcript reconciliation without queued catch-up scans.
package cli

import (
	"context"
	"time"
)

// Coalesce active transcript writes and avoid continually traversing host
// bind mounts. Hooks remain immediate; transcripts reconcile every 15 seconds
// after the previous scan completes.
const collectionWatchInterval = 15 * time.Second

func watchCollection(ctx context.Context, interval time.Duration, scan func() error, onError func(error)) {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if ctx.Err() != nil {
				return
			}
			if err := scan(); err != nil && ctx.Err() == nil {
				onError(err)
			}
			if ctx.Err() != nil {
				return
			}
			timer.Reset(interval)
		}
	}
}
