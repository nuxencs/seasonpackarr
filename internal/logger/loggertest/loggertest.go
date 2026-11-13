// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package loggertest provides a logger.Logger that records JSON events for tests.
package loggertest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/logger"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// Logger records every event as one JSON line. Reads are safe while other
// goroutines log, such as the config file watcher.
type Logger struct {
	mu  sync.Mutex
	out bytes.Buffer
	log zerolog.Logger
}

var _ logger.Logger = (*Logger)(nil)

// New returns a Logger that records events in memory.
func New() *Logger {
	l := &Logger{}
	l.log = zerolog.New(lockedWriter{l})
	return l
}

type lockedWriter struct{ l *Logger }

func (w lockedWriter) Write(p []byte) (int, error) {
	w.l.mu.Lock()
	defer w.l.mu.Unlock()
	return w.l.out.Write(p)
}

func (l *Logger) Log() *zerolog.Event { return l.log.Log() }

// Fatal panics instead of exiting, so a test can observe it.
func (l *Logger) Fatal() *zerolog.Event        { return l.log.Panic() }
func (l *Logger) Err(err error) *zerolog.Event { return l.log.Err(err) }
func (l *Logger) Error() *zerolog.Event        { return l.log.Error() }
func (l *Logger) Warn() *zerolog.Event         { return l.log.Warn() }
func (l *Logger) Info() *zerolog.Event         { return l.log.Info() }
func (l *Logger) Trace() *zerolog.Event        { return l.log.Trace() }
func (l *Logger) Debug() *zerolog.Event        { return l.log.Debug() }
func (l *Logger) With() zerolog.Context        { return l.log.With() }

// SetLogLevel does nothing: the Logger records every level so tests can assert on it.
func (l *Logger) SetLogLevel(string) {}

// String returns every recorded event as JSON lines.
func (l *Logger) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.out.String()
}

// Reset drops the recorded events.
func (l *Logger) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.out.Reset()
}

// Event is one decoded log event.
type Event map[string]any

// Events is a snapshot of the recorded events.
type Events []Event

// Events decodes the events recorded so far. Later events do not change the
// returned snapshot.
func (l *Logger) Events(t testing.TB) Events {
	t.Helper()
	var events Events
	for line := range bytes.SplitSeq([]byte(l.String()), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event Event
		require.NoError(t, json.Unmarshal(line, &event))
		events = append(events, event)
	}
	return events
}

// Require returns the first event with message.
func (e Events) Require(t testing.TB, message string) Event {
	t.Helper()
	return e.require(t, fmt.Sprintf("message %q", message), func(event Event) bool {
		return event["message"] == message
	})
}

// RequireField returns the first event with message and the string field set
// to value.
func (e Events) RequireField(t testing.TB, message, field, value string) Event {
	t.Helper()
	return e.require(t, fmt.Sprintf("message %q with %s=%q", message, field, value), func(event Event) bool {
		return event["message"] == message && event[field] == value
	})
}

func (e Events) require(t testing.TB, want string, match func(Event) bool) Event {
	t.Helper()
	for _, event := range e {
		if match(event) {
			return event
		}
	}
	require.FailNow(t, "missing log event", "%s in %#v", want, e)
	return nil
}
