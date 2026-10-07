// @module CollectionWatchTests verifies bounded background pacing and cancellation.
package cli

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWatchCollectionWaitsAfterSlowPassAndStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	interval := 20 * time.Millisecond
	calls, warnings := 0, 0
	lastEnd := time.Now()
	watchCollection(ctx, interval, func() error {
		if elapsed := time.Since(lastEnd); elapsed < interval {
			t.Fatalf("pass started after %s; need %s rest", elapsed, interval)
		}
		calls++
		// A scan slower than the interval must not queue a catch-up scan.
		time.Sleep(2 * interval)
		lastEnd = time.Now()
		if calls == 3 {
			cancel()
		}
		return errors.New("retryable import failure")
	}, func(error) { warnings++ })
	if calls != 3 || warnings != 2 {
		t.Fatalf("calls=%d warnings=%d", calls, warnings)
	}
}

func TestWatchCollectionCancelledBeforeFirstScan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	watchCollection(ctx, time.Hour, func() error { t.Fatal("cancelled collector scanned"); return nil }, func(error) { t.Fatal("cancelled collector reported error") })
}
