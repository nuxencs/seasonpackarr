// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package daemontest holds the helpers that tests against real torrent client
// daemons share: the settings gate, bounded waits, the season pack and torrent
// builders, the seeder, and the checks of reused and downloaded pack files. The
// test harness in test/harness starts the daemons.
package daemontest

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// envRequireDaemons set to 1 fails tests whose gate settings are missing, so a
// harness configuration mistake cannot pass as a run of skips.
const envRequireDaemons = "SEASONPACKARR_TEST_REQUIRE_DAEMONS"

// Timeout bounds every wait for a daemon.
const Timeout = 30 * time.Second

// RequireSettings skips the test unless every key is set. In strict mode it
// fails the test instead.
func RequireSettings(t *testing.T, keys ...string) {
	t.Helper()
	var missing []string
	for _, key := range keys {
		if os.Getenv(key) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		message := fmt.Sprintf("set %s to run this test", strings.Join(missing, " and "))
		if os.Getenv(envRequireDaemons) == "1" {
			t.Fatalf("%s: %s=1 requires every daemon setting", message, envRequireDaemons)
		}
		t.Skip(message)
	}
}

// CleanupContext is for t.Cleanup functions: t.Context is canceled before they run.
func CleanupContext(t *testing.T) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(t.Context()), Timeout)
}

// WaitFor calls read until done reports true, ctx ends, or Timeout ends. It
// returns the last value and whether done reported true, so the caller can
// assert on the final daemon state.
func WaitFor[T any](ctx context.Context, read func() T, done func(T) bool) (T, bool) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	for {
		value := read()
		if done(value) {
			return value, true
		}
		select {
		case <-ctx.Done():
			return value, false
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// AssertRemoved waits until present reports that the daemon no longer holds
// hash. Cleanups use it so a test cannot leave a torrent behind for the next run.
func AssertRemoved(t *testing.T, hash string, present func() (bool, error)) {
	t.Helper()
	var err error
	// WaitFor bounds the wait; t.Context is already canceled in a cleanup.
	_, removed := WaitFor(context.WithoutCancel(t.Context()), func() bool {
		var found bool
		found, err = present()
		return found
	}, func(found bool) bool { return err != nil || !found })
	if assert.NoError(t, err, "check removal of torrent %s", hash) {
		assert.True(t, removed, "daemon still holds the torrent %s after removal", hash)
	}
}
