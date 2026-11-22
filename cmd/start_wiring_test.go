// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The in-process start tests run start through execute, with a temporary config directory and a literal environment.
// They check the wiring of start (load, logger, watch, database, server) through what a user sees.

const (
	startTestToken = "start-test-token"
	// startWait bounds the wait for start to return. It is longer than serverShutdownTimeout, so a slow shutdown fails
	// as a shutdown.
	startWait = serverShutdownTimeout + 5*time.Second
	// conditionWait bounds each wait for a state that start reaches in a short time, such as a served port or a reload.
	conditionWait = 5 * time.Second
)

// newStartConfig returns a valid config.yaml that serves on the loopback port and logs at debug level into logPath.
// clients is the YAML of the clients map.
func newStartConfig(port int, token, logPath, clients string) string {
	return fmt.Sprintf("host: 127.0.0.1\nport: %d\napiToken: %s\nlogLevel: DEBUG\nlogPath: %q\nclients:\n%s", port, token, logPath, clients)
}

// newQbitClientConfig returns the YAML of a qBittorrent client without credentials, so its adapter connects without a
// login. A fake API at a loopback port answers each torrent read with an empty list.
func newQbitClientConfig(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v2/torrents/info", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "[]")
	})
	api := httptest.NewServer(mux)
	t.Cleanup(api.Close)
	return fmt.Sprintf("  default:\n    type: qbittorrent\n    host: %s\n    username: \"\"\n    password: \"\"\n    import:\n      category: tv\n", api.URL)
}

// reservePort returns a free loopback port. Another process can take it between the close and the listen of start.
// The tests accept this short window, so the server needs no listener input.
func reservePort(t *testing.T) int {
	t.Helper()
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	return port
}

// keepLogLevel restores the global log level that start sets through the service logger.
func keepLogLevel(t *testing.T) {
	t.Helper()
	previous := zerolog.GlobalLevel()
	t.Cleanup(func() { zerolog.SetGlobalLevel(previous) })
}

func writeConfig(t *testing.T, dir, contents string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(contents), 0o600))
}

// startClient sends each request on its own connection. A pooled connection that no request used delays the shutdown
// of the server by 5 seconds, because http.Server.Shutdown waits that long for a new connection. The timeout keeps a
// request that gets no response from blocking the deadline check of waitFor.
var startClient = &http.Client{
	Timeout:   time.Second,
	Transport: &http.Transport{DisableKeepAlives: true},
}

// startRun is one start command that runs in-process until its context ends.
type startRun struct {
	port   int
	cancel context.CancelFunc
	// finished closes when execute returns. Its close orders the writes of the result fields before the reads.
	finished       chan struct{}
	code           int
	stdout, stderr string
}

// launchStart runs start with dir as its config directory and environ as its environment, and returns at once.
func launchStart(t *testing.T, dir string, port int, environ []string) *startRun {
	t.Helper()
	keepLogLevel(t)
	ctx, cancel := context.WithCancel(t.Context())
	run := &startRun{port: port, cancel: cancel, finished: make(chan struct{})}
	go func() {
		defer close(run.finished)
		var stdout, stderr bytes.Buffer
		run.code = execute(ctx, []string{"start", "--config", dir}, environ, &stdout, &stderr)
		run.stdout, run.stderr = stdout.String(), stderr.String()
	}()
	t.Cleanup(func() {
		cancel()
		run.wait(t)
	})
	return run
}

// runStart runs start with an empty environment and waits until it serves on port.
func runStart(t *testing.T, dir string, port int) *startRun {
	t.Helper()
	run := launchStart(t, dir, port, nil)
	require.True(t, waitFor(conditionWait, func() bool {
		// The result fields are safe to read only after finished closed.
		if run.returned() {
			require.FailNow(t, "start returned before it served", "exit code %d: %s", run.code, run.stderr)
		}
		return run.live(t)
	}), "start did not serve")
	return run
}

func (r *startRun) returned() bool {
	select {
	case <-r.finished:
		return true
	default:
		return false
	}
}

// wait returns the exit code of start after it returned.
func (r *startRun) wait(t *testing.T) int {
	t.Helper()
	select {
	case <-r.finished:
		return r.code
	case <-time.After(startWait):
		require.Fail(t, "start did not return")
		return 0
	}
}

// live reports whether the liveness endpoint answers.
func (r *startRun) live(t *testing.T) bool {
	t.Helper()
	return r.send(t, http.MethodGet, "/api/healthz/liveness", "") == http.StatusOK
}

// post sends a candidate check with an empty body and token in the X-API-Token header, and returns the status code.
// An authorized request fails with the decoding error, so the code shows whether start accepted the token.
func (r *startRun) post(t *testing.T, token string) int {
	t.Helper()
	return r.send(t, http.MethodPost, "/api/candidate", token)
}

// send returns the status code of the response, or 0 when no server answers.
func (r *startRun) send(t *testing.T, method, path, token string) int {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, fmt.Sprintf("http://127.0.0.1:%d%s", r.port, path), nil)
	require.NoError(t, err)
	if token != "" {
		request.Header.Set("X-API-Token", token)
	}
	response, err := startClient.Do(request)
	if err != nil {
		return 0
	}
	require.NoError(t, response.Body.Close())
	return response.StatusCode
}

// waitFor reports whether condition became true within timeout.
func waitFor(timeout time.Duration, condition func() bool) bool {
	deadline := time.Now().Add(timeout)
	for !condition() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
	return true
}

// logEvent is the part of a log file line that the tests read.
type logEvent struct {
	Message    string   `json:"message"`
	ClientName string   `json:"clientname"`
	Clients    []string `json:"clients"`
}

// hasLogEvent reports whether the log file at path holds an event that match accepts.
func hasLogEvent(t *testing.T, path string, match func(logEvent) bool) bool {
	t.Helper()
	contents, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	require.NoError(t, err)
	for line := range bytes.Lines(contents) {
		var event logEvent
		// The last line can be half written, and the tests read only complete events.
		if err := jsonv2.Unmarshal(line, &event, jsontext.AllowDuplicateNames(true)); err == nil && match(event) {
			return true
		}
	}
	return false
}

func TestStartCommand_ReturnsConfigLoadError(t *testing.T) {
	secrets := map[string]string{
		"apiToken":        "sentinel-start-api-token",
		"client password": "sentinel-start-client-password",
		"Prowlarr apiKey": "sentinel-start-prowlarr-api-key",
		"Discord webhook": "sentinel-start-discord-webhook-token",
		"Shoutrrr URL":    "sentinel-start-ntfy-topic",
		"port variable":   "sentinel-start-port-value",
	}
	valid := `host: 127.0.0.1
port: {port}
apiToken: sentinel-start-api-token
clients:
  default:
    type: qbittorrent
    host: http://qbittorrent
    port: 8080
    password: sentinel-start-client-password
    import:
      category: tv-hd
search:
  prowlarrURL: http://prowlarr:9696
  apiKey: sentinel-start-prowlarr-api-key
notifications:
  discord: https://discord.com/api/webhooks/123/sentinel-start-discord-webhook-token
  shoutrrr: ["ntfy://ntfy.sh/sentinel-start-ntfy-topic"]
`
	tests := map[string]struct {
		replace []string
		env     []string
		legacy  bool
		want    string
	}{
		"YAML syntax error after the apiToken": {
			replace: []string{"clients:\n", "clients: [\n"},
			want:    "Error: reading config file ",
		},
		"port next to the password is not a number": {
			replace: []string{"port: 8080", "port: not-a-port"},
			want:    "Error: decoding config file ",
		},
		"password is a map": {
			replace: []string{"password: sentinel-start-client-password", "password: {value: sentinel-start-client-password}"},
			want:    "Error: decoding config file ",
		},
		"Shoutrrr URL is invalid": {
			replace: []string{"ntfy://ntfy.sh/sentinel-start-ntfy-topic", "gotify://gotify.example/sentinel-start-ntfy-topic"},
			want:    "notifications.shoutrrr[0] (gotify)",
		},
		"port variable is not a number": {
			env:  []string{"SEASONPACKARR__PORT=sentinel-start-port-value"},
			want: "environment variable SEASONPACKARR__PORT is not an integer",
		},
		"apiToken variable ends with a line break": {
			env:  []string{"SEASONPACKARR__API_TOKEN=sentinel-start-api-token\n"},
			want: "environment variable SEASONPACKARR__API_TOKEN contains a control character",
		},
		"client password variable has a control character": {
			env:  []string{"SEASONPACKARR__CLIENTS_DEFAULT_PASSWORD=sentinel-start-client-password\x01"},
			want: "environment variable SEASONPACKARR__CLIENTS_DEFAULT_PASSWORD contains a control character",
		},
		"legacy config.toml": {
			legacy: true,
			want:   "Error: A legacy 'config.toml' file has been detected.",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			keepLogLevel(t)
			port := reservePort(t)
			invalid := strings.ReplaceAll(valid, "{port}", fmt.Sprint(port))
			if tt.replace != nil {
				replaced := strings.NewReplacer(tt.replace...).Replace(invalid)
				require.NotEqual(t, invalid, replaced, "the replacement matched nothing")
				invalid = replaced
			}
			dir := t.TempDir()
			writeConfig(t, dir, invalid)
			if tt.legacy {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"), nil, 0o600))
			}
			environ := append([]string{"SEASONPACKARR__UNKNOWN_SETTING=value"}, tt.env...)

			run := launchStart(t, dir, port, environ)

			// The port must stay closed until start returns, so a config that start wrongly accepts fails at once.
			require.True(t, waitFor(startWait, func() bool {
				require.False(t, run.live(t), "start serves an invalid config")
				return run.returned()
			}), "start did not return")
			require.Equal(t, 1, run.wait(t), run.stderr)
			assert.Empty(t, run.stdout)
			// An unknown variable can explain the load error, so start prints it before the error.
			warning, errLine, ok := strings.Cut(run.stderr, "\n")
			require.True(t, ok, run.stderr)
			assert.Equal(t, "warning: unknown environment variable SEASONPACKARR__UNKNOWN_SETTING; seasonpackarr ignores it", warning)
			assert.Equal(t, 1, strings.Count(run.stderr, "warning:"), run.stderr)
			// The secret checks prove nothing without the load error.
			require.True(t, strings.HasPrefix(errLine, "Error: "), run.stderr)
			require.Contains(t, errLine, tt.want, run.stderr)
			for kind, secret := range secrets {
				assert.NotContains(t, run.stderr, secret, "start output holds the %s", kind)
			}
			assert.NoFileExists(t, filepath.Join(dir, "seasonpackarr.db"))
		})
	}
}

func TestStartCommand_ServesAuthenticatedEndpoint(t *testing.T) {
	dir := t.TempDir()
	port := reservePort(t)
	writeConfig(t, dir, newStartConfig(port, startTestToken, filepath.Join(dir, "seasonpackarr.log"), "  {}\n"))

	run := runStart(t, dir, port)

	assert.Equal(t, http.StatusUnauthorized, run.post(t, ""))
	assert.Equal(t, http.StatusUnauthorized, run.post(t, "wrong-token"))
	assert.Equal(t, domain.StatusDecodingError.Code(), run.post(t, startTestToken))
	assert.FileExists(t, filepath.Join(dir, "seasonpackarr.db"))
}

// TestStartCommand_AppliesReloadedConfig fails when start does not watch the config file, or when the reload does
// not reach the inventory that the server uses.
func TestStartCommand_AppliesReloadedConfig(t *testing.T) {
	dir := t.TempDir()
	port := reservePort(t)
	logPath := filepath.Join(dir, "seasonpackarr.log")
	writeConfig(t, dir, newStartConfig(port, startTestToken, logPath, newQbitClientConfig(t)))
	run := runStart(t, dir, port)
	// The startup read stores the connection of the client, and the reload must drop it.
	require.True(t, waitFor(conditionWait, func() bool {
		return hasLogEvent(t, logPath, func(event logEvent) bool {
			return event.Message == "client inventory ready" && event.ClientName == "default"
		})
	}), "the startup read did not read the client")

	writeConfig(t, dir, newStartConfig(port, "reloaded-token", logPath, "  {}\n"))

	require.True(t, waitFor(conditionWait, func() bool {
		return run.post(t, "reloaded-token") == domain.StatusDecodingError.Code()
	}), "start did not accept the reloaded apiToken")
	assert.Equal(t, http.StatusUnauthorized, run.post(t, startTestToken))
	assert.True(t, waitFor(conditionWait, func() bool {
		return hasLogEvent(t, logPath, func(event logEvent) bool {
			return event.Message == "dropped caches of removed or changed clients" && slices.Equal(event.Clients, []string{"default"})
		})
	}), "the reload did not drop the connection of the removed client")
}

func TestStartCommand_ShutsDownWhenContextEnds(t *testing.T) {
	dir := t.TempDir()
	port := reservePort(t)
	logPath := filepath.Join(dir, "seasonpackarr.log")
	writeConfig(t, dir, newStartConfig(port, startTestToken, logPath, "  {}\n"))
	run := runStart(t, dir, port)

	run.cancel()

	assert.Equal(t, 0, run.wait(t), run.stderr)
	assert.Empty(t, run.stdout+run.stderr)
	assert.False(t, run.live(t), "start serves after it returned")
	assert.True(t, hasLogEvent(t, logPath, func(event logEvent) bool {
		return event.Message == "Shutting down the server gracefully..."
	}))
}
