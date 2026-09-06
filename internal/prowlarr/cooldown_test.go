// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package prowlarr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClient_CooldownResponses(t *testing.T) {
	for _, status := range []int{429, 408, 500, 502, 503, 504} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			fallback := 10 * time.Minute
			for _, header := range []string{"", "bad", "0", "-1", "30", "999999999999999999999", "7200", time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat), time.Now().Add(2 * time.Hour).UTC().Format(http.TimeFormat), time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)} {
				t.Run(header, func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Retry-After", header)
						w.WriteHeader(status)
						fmt.Fprint(w, "secret-key")
					}))
					t.Cleanup(server.Close)
					client, err := New(server.URL, "secret-key", 0)
					require.NoError(t, err)
					_, err = client.Indexers(t.Context())
					failure, ok := errors.AsType[*CooldownError](err)
					require.True(t, ok, "%v", err)
					require.NotContains(t, err.Error(), "secret-key")
					want := time.Now().Add(fallback)
					if header == "0" {
						want = time.Now()
					} else if header == "30" {
						want = time.Now().Add(30 * time.Second)
					} else if header == "7200" {
						want = time.Now().Add(2 * time.Hour)
					} else if date, err := http.ParseTime(header); err == nil {
						want = date
					}
					require.WithinDuration(t, want, failure.Until, time.Second)
				})
			}
		})
	}
}

type cooldownTransport func(*http.Request) (*http.Response, error)

func (f cooldownTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type failedResponseBody struct{}

func (failedResponseBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (failedResponseBody) Close() error             { return nil }

func TestClient_CooldownTransportAndCancellation(t *testing.T) {
	for _, tt := range []struct {
		name         string
		transport    cooldownTransport
		canceled     bool
		wantCooldown bool
	}{
		{name: "transport", wantCooldown: true, transport: func(*http.Request) (*http.Response, error) { return nil, errors.New("secret connection failure") }},
		{name: "response read", wantCooldown: true, transport: func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: failedResponseBody{}}, nil
		}},
		{name: "permanent error", transport: func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("secret"))}, nil
		}},
		{name: "canceled", canceled: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client, err := New("http://prowlarr.invalid", "key", 0)
			require.NoError(t, err)
			client.http.Transport = tt.transport
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.canceled {
				cancel()
			}
			_, err = client.Indexers(ctx)
			require.Error(t, err)
			_, ok := errors.AsType[*CooldownError](err)
			require.Equal(t, tt.wantCooldown, ok)
			require.NotContains(t, err.Error(), "secret")
			if tt.canceled {
				require.ErrorIs(t, err, context.Canceled)
			}
		})
	}
}
