// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAPI_RedirectDoesNotForwardCredentials(t *testing.T) {
	isolateCLI(t)
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	code, stdout, _ := runCLI(t, "candidate", "Series.S01", "--url", server.URL, "--api", "secret")
	require.Equal(t, 1, code)
	require.Contains(t, stdout, "redirect")
	require.Zero(t, calls.Load())
}

func TestAPI_ServerErrorRedactsTokenAndControlCharacters(t *testing.T) {
	isolateCLI(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		fmt.Fprint(w, `{"error":"token=secret\u001b[31m failed"}`)
	}))
	defer server.Close()
	code, stdout, stderr := runCLI(t, "candidate", "Series.S01", "--url", server.URL, "--api", "secret")
	require.Equal(t, 1, code)
	require.NotContains(t, stdout+stderr, "secret")
	require.NotContains(t, stdout+stderr, "\x1b")
	require.Contains(t, stdout, "[redacted]")
}

func TestAPI_CancellationStopsSearch(t *testing.T) {
	isolateCLI(t)
	started, stopped := make(chan struct{}), make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the POST body so the server can observe a disconnected client.
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		select {
		case <-r.Context().Done():
			close(stopped)
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var stdout, stderr bytes.Buffer
	result := make(chan int, 1)
	go func() { result <- execute(ctx, []string{"search", "--url", server.URL, "--dry-run"}, &stdout, &stderr) }()
	<-started
	cancel()
	select {
	case code := <-result:
		require.Equal(t, 1, code)
		require.Contains(t, stderr.String(), "context canceled")
	case <-time.After(3 * time.Second):
		t.Fatal("search did not stop after cancellation")
	}
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("service did not observe cancellation")
	}
}

func TestAPI_RejectsOversizedResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", (32<<20)+1))
	}))
	defer server.Close()
	api := &apiClient{baseURL: server.URL}
	_, err := api.post(t.Context(), "/api/search", strings.NewReader("{}"), 0)
	require.ErrorContains(t, err, "exceeds 32 MiB")
}
