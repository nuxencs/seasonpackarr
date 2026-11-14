// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package notification

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger/loggertest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDestination is a local HTTP server that records the requests that it gets.
type fakeDestination struct {
	server   *httptest.Server
	requests chan capturedRequest
}

type capturedRequest struct {
	path string
	body []byte
}

func newFakeDestination(t *testing.T, status int) *fakeDestination {
	t.Helper()
	f := &fakeDestination{requests: make(chan capturedRequest, 10)}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		f.requests <- capturedRequest{path: r.URL.Path, body: body}
		w.WriteHeader(status)
	}))
	t.Cleanup(f.server.Close)
	return f
}

// genericURL is a Shoutrrr generic URL that posts {"title", "message"} JSON to the server.
func (f *fakeDestination) genericURL(path, query string) string {
	return "generic://" + strings.TrimPrefix(f.server.URL, "http://") + path + "?template=json&disabletls=yes" + query
}

// discordURL is a webhook URL on the server.
func (f *fakeDestination) discordURL() string {
	return f.server.URL + "/api/webhooks/123/token"
}

// requireRequest returns the one request that the server got.
func (f *fakeDestination) requireRequest(t *testing.T) capturedRequest {
	t.Helper()
	select {
	case request := <-f.requests:
		return request
	default:
		require.FailNow(t, "the destination got no request")
		return capturedRequest{}
	}
}

func (f *fakeDestination) assertNoRequest(t *testing.T) {
	t.Helper()
	select {
	case request := <-f.requests:
		assert.Failf(t, "the destination got a request", "path %s", request.path)
	default:
	}
}

type genericPayload struct {
	Title   string `json:"title"`
	Message string `json:"message"`
}

func requireGenericPayload(t *testing.T, request capturedRequest) genericPayload {
	t.Helper()
	var payload genericPayload
	require.NoError(t, json.Unmarshal(request.body, &payload), string(request.body))
	return payload
}

func newTestSender(notifications domain.Notifications) (domain.Sender, *loggertest.Logger) {
	log := loggertest.New()
	return NewSender(log, fakeConfig{config: domain.Config{Notifications: notifications}}), log
}

var testPayload = domain.NotificationPayload{
	ReleaseName: "Series.S01.1080p.WEB-DL-GRP",
	Client:      "default",
	Action:      "Import",
}

func TestSender_SendsPlainTextToShoutrrrURL(t *testing.T) {
	destination := newFakeDestination(t, http.StatusOK)
	sender, _ := newTestSender(domain.Notifications{
		NotificationLevel: []string{domain.NotificationLevelMatch},
		Shoutrrr:          []string{destination.genericURL("/hook", "")},
	})

	require.NoError(t, sender.Send(t.Context(), domain.StatusSuccessfulHardlink, testPayload))

	payload := requireGenericPayload(t, destination.requireRequest(t))
	assert.Equal(t, "Successful match", payload.Title)
	assert.Equal(t, "Release Name: Series.S01.1080p.WEB-DL-GRP\nClient: default\nAction: Import", payload.Message)
}

func TestSender_SendsToEveryDestination(t *testing.T) {
	discord := newFakeDestination(t, http.StatusNoContent)
	first := newFakeDestination(t, http.StatusOK)
	second := newFakeDestination(t, http.StatusOK)
	sender, _ := newTestSender(domain.Notifications{
		NotificationLevel: []string{domain.NotificationLevelMatch},
		Discord:           discord.discordURL(),
		Shoutrrr:          []string{first.genericURL("/first", ""), second.genericURL("/second", "")},
	})

	require.NoError(t, sender.Send(t.Context(), domain.StatusSuccessfulHardlink, testPayload))

	var embed DiscordMessage
	require.NoError(t, json.Unmarshal(discord.requireRequest(t).body, &embed))
	require.Len(t, embed.Embeds, 1)
	assert.Equal(t, "Successful match", embed.Embeds[0].Title)
	assert.Equal(t, "/first", first.requireRequest(t).path)
	assert.Equal(t, "/second", second.requireRequest(t).path)
}

func TestSender_FailedDestinationDoesNotStopOthers(t *testing.T) {
	discord := newFakeDestination(t, http.StatusNoContent)
	failing := newFakeDestination(t, http.StatusInternalServerError)
	working := newFakeDestination(t, http.StatusOK)
	release := make(chan struct{})
	hanging := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(hanging.Close)
	// Cleanups run in reverse order, so the handler returns before Close waits for it.
	t.Cleanup(func() { close(release) })
	hangingURL := "generic://" + strings.TrimPrefix(hanging.URL, "http://") + "/hang?disabletls=yes"

	sender, _ := newTestSender(domain.Notifications{
		NotificationLevel: []string{domain.NotificationLevelMatch},
		Discord:           discord.discordURL(),
		Shoutrrr:          []string{failing.genericURL("/fail", ""), hangingURL, working.genericURL("/work", "")},
	})
	sender.(*notificationSender).timeout = 200 * time.Millisecond

	err := sender.Send(t.Context(), domain.StatusSuccessfulHardlink, testPayload)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "shoutrrr[0] (generic): ")
	assert.Contains(t, err.Error(), "shoutrrr[1] (generic): ")
	assert.NotContains(t, err.Error(), "shoutrrr[2]")
	assert.NotContains(t, err.Error(), "discord")
	assert.Equal(t, "/fail", failing.requireRequest(t).path)
	assert.Equal(t, "/work", working.requireRequest(t).path)
	discord.requireRequest(t)
}

func TestSender_LevelFiltersEveryDestination(t *testing.T) {
	discord := newFakeDestination(t, http.StatusNoContent)
	shoutrrr := newFakeDestination(t, http.StatusOK)
	sender, _ := newTestSender(domain.Notifications{
		NotificationLevel: []string{domain.NotificationLevelError},
		Discord:           discord.discordURL(),
		Shoutrrr:          []string{shoutrrr.genericURL("/hook", "")},
	})

	require.NoError(t, sender.Send(t.Context(), domain.StatusRlsGrpMismatch, testPayload))

	discord.assertNoRequest(t)
	shoutrrr.assertNoRequest(t)
}

func TestSender_ErrorLineOnlyForErrorLevel(t *testing.T) {
	tests := map[string]struct {
		statusCode domain.StatusCode
		want       string
	}{
		"error level": {
			statusCode: domain.StatusFailedHardlink,
			want:       "Release Name: Series.S01.1080p.WEB-DL-GRP\nClient: default\nAction: Import\nError: disk full",
		},
		"info level": {
			statusCode: domain.StatusRlsGrpMismatch,
			want:       "Release Name: Series.S01.1080p.WEB-DL-GRP\nClient: default\nAction: Import",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			destination := newFakeDestination(t, http.StatusOK)
			sender, _ := newTestSender(domain.Notifications{
				NotificationLevel: []string{domain.NotificationLevelInfo, domain.NotificationLevelError},
				Shoutrrr:          []string{destination.genericURL("/hook", "")},
			})
			payload := testPayload
			payload.Error = errors.New("disk full")

			require.NoError(t, sender.Send(t.Context(), tt.statusCode, payload))

			assert.Equal(t, tt.want, requireGenericPayload(t, destination.requireRequest(t)).Message)
		})
	}
}

// newEchoDestination rejects each request and quotes it in the response, so a
// service error that quotes the response would hold every secret of the request.
func newEchoDestination(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, `{"error":"rejected","message":%q}`, fmt.Sprint(r.URL.String(), r.Header, string(body)))
	}))
	t.Cleanup(server.Close)
	return server
}

// fakeRedirectTransport sends every request to server, for services with a fixed API host.
type fakeRedirectTransport struct{ server *httptest.Server }

func (f fakeRedirectTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	target, err := url.Parse(f.server.URL)
	if err != nil {
		return nil, err
	}
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
	return http.DefaultTransport.RoundTrip(r)
}

func TestSender_SendErrorsHoldNoSecret(t *testing.T) {
	echo := newEchoDestination(t)
	host := strings.TrimPrefix(echo.URL, "http://")
	tests := map[string]struct {
		url     string
		secrets []string
	}{
		"gotify token": {
			url:     "gotify://" + host + "/Asentinelgotify?disabletls=yes",
			secrets: []string{"Asentinelgotify"},
		},
		"telegram token and chats": {
			url:     "telegram://123456:sentinel-telegram-token@telegram?chats=sentinel-telegram-chat,-1001&apiurl=" + echo.URL,
			secrets: []string{"sentinel-telegram-token", "sentinel-telegram-chat"},
		},
		"ntfy topic": {
			url:     "ntfy://" + host + "/sentinel-ntfy-topic?disabletls=yes",
			secrets: []string{"sentinel-ntfy-topic"},
		},
		"discord token": {
			url:     "discord://sentinel-discord-token@123456789",
			secrets: []string{"sentinel-discord-token"},
		},
		"generic token in the query": {
			url:     "generic://" + host + "/hook?disabletls=yes&token=sentinel-generic-token",
			secrets: []string{"sentinel-generic-token"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			sender, _ := newTestSender(domain.Notifications{
				NotificationLevel: []string{domain.NotificationLevelMatch},
				Shoutrrr:          []string{tt.url},
			})
			sender.(*notificationSender).shoutrrrClient = &http.Client{Transport: fakeRedirectTransport{echo}}

			err := sender.Send(t.Context(), domain.StatusSuccessfulHardlink, testPayload)

			// The send must fail at the destination, or the checks below prove nothing.
			require.Error(t, err)
			require.Contains(t, err.Error(), "shoutrrr[0] (")
			for _, secret := range tt.secrets {
				assert.NotContains(t, err.Error(), secret)
			}
		})
	}
}

// fakeReloadingConfig is a config provider whose config the test replaces, like a hot reload.
type fakeReloadingConfig struct {
	mu     sync.Mutex
	config domain.Config
}

func (f *fakeReloadingConfig) Snapshot() domain.Config {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.config
}

func (f *fakeReloadingConfig) store(config domain.Config) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.config = config
}

func TestSender_FollowsConfigReload(t *testing.T) {
	before := newFakeDestination(t, http.StatusOK)
	after := newFakeDestination(t, http.StatusOK)
	notifications := domain.Notifications{
		NotificationLevel: []string{domain.NotificationLevelMatch},
		Shoutrrr:          []string{before.genericURL("/before", "")},
	}
	cfg := &fakeReloadingConfig{config: domain.Config{Notifications: notifications}}
	sender := NewSender(loggertest.New(), cfg)
	require.NoError(t, sender.Send(t.Context(), domain.StatusSuccessfulHardlink, testPayload))

	notifications.Shoutrrr = []string{after.genericURL("/after", "")}
	cfg.store(domain.Config{Notifications: notifications})
	require.NoError(t, sender.Send(t.Context(), domain.StatusSuccessfulHardlink, testPayload))

	assert.Equal(t, "/before", before.requireRequest(t).path)
	before.assertNoRequest(t)
	assert.Equal(t, "/after", after.requireRequest(t).path)
}

// TestSender_KeepsDestinationsAfterRejectedReload uses the real config watcher,
// so the old destination must still get notifications after an invalid edit.
func TestSender_KeepsDestinationsAfterRejectedReload(t *testing.T) {
	t.Setenv("SEASONPACKARR__DISABLE_CONFIG_FILE", "")
	t.Setenv("SEASONPACKARR__NOTIFICATIONS_SHOUTRRR", "")
	destination := newFakeDestination(t, http.StatusOK)
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")
	writeConfig := func(shoutrrrURL string) {
		t.Helper()
		contents := fmt.Sprintf("notifications:\n  notificationLevel: [MATCH]\n  shoutrrr: [%q]\n", shoutrrrURL)
		require.NoError(t, os.WriteFile(configFile, []byte(contents), 0o600))
	}
	writeConfig(destination.genericURL("/before", ""))
	cfg := config.New(dir, "test")
	log := loggertest.New()
	_, err := cfg.DynamicReload(log)
	require.NoError(t, err)
	sender := NewSender(log, cfg)

	writeConfig("gotify://gotify.example/invalid-token")
	require.Eventually(t, func() bool {
		return strings.Contains(log.String(), "config reload rejected")
	}, 3*time.Second, 10*time.Millisecond, log.String())
	require.NoError(t, sender.Send(t.Context(), domain.StatusSuccessfulHardlink, testPayload))

	assert.Equal(t, "/before", destination.requireRequest(t).path)
}
