// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package loggertest provides a logger.Logger that records JSON events for tests.
package loggertest

import (
	"bytes"
	"encoding/json"
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
func (l *Logger) SetLogLevel(string)           {}

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

// Events decodes every recorded event.
func (l *Logger) Events(t testing.TB) []map[string]any {
	t.Helper()
	var events []map[string]any
	for line := range bytes.SplitSeq([]byte(l.String()), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event map[string]any
		require.NoError(t, json.Unmarshal(line, &event))
		events = append(events, event)
	}
	return events
}

// RequireEvent returns the first event with message.
func RequireEvent(t testing.TB, events []map[string]any, message string) map[string]any {
	t.Helper()
	for _, event := range events {
		if event["message"] == message {
			return event
		}
	}
	require.FailNow(t, "missing log event", "message %q in %#v", message, events)
	return nil
}

// RequireEventField returns the first event with message and field set to value.
func RequireEventField(t testing.TB, events []map[string]any, message, field string, value any) map[string]any {
	t.Helper()
	for _, event := range events {
		if event["message"] == message && event[field] == value {
			return event
		}
	}
	require.FailNow(t, "missing log event", "message %q with %s=%v in %#v", message, field, value, events)
	return nil
}
