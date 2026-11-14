// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFakeNotificationDestination answers each request with status and counts the requests.
func newFakeNotificationDestination(t *testing.T, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		requests.Add(1)
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func genericURL(server *httptest.Server, query string) string {
	return "generic://" + strings.TrimPrefix(server.URL, "http://") + "/hook?disabletls=yes" + query
}

func TestNotificationTestCommand_SendsToEachDestination(t *testing.T) {
	isolateCLI(t)
	discord, discordRequests := newFakeNotificationDestination(t, http.StatusNoContent)
	shoutrrr, shoutrrrRequests := newFakeNotificationDestination(t, http.StatusOK)
	t.Setenv("SEASONPACKARR__NOTIFICATIONS_DISCORD", discord.URL+"/api/webhooks/123/token")
	t.Setenv("SEASONPACKARR__NOTIFICATIONS_SHOUTRRR", genericURL(shoutrrr, ""))
	// The test notification ignores the notification level.
	t.Setenv("SEASONPACKARR__NOTIFICATIONS_NOTIFICATION_LEVEL", "INFO")

	code, stdout, stderr := runCLI(t, "notification", "test")

	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "discord: ok\nshoutrrr[0] (generic): ok\n", stdout)
	assert.Equal(t, int32(1), discordRequests.Load())
	assert.Equal(t, int32(1), shoutrrrRequests.Load())
}

func TestNotificationTestCommand_FailedSendExitsNonZero(t *testing.T) {
	isolateCLI(t)
	working, workingRequests := newFakeNotificationDestination(t, http.StatusOK)
	failing, _ := newFakeNotificationDestination(t, http.StatusBadRequest)
	const secret = "sentinel-notification-test-token"
	t.Setenv("SEASONPACKARR__NOTIFICATIONS_SHOUTRRR", genericURL(failing, "&token="+secret)+" "+genericURL(working, ""))

	code, stdout, stderr := runCLI(t, "notification", "test")

	assert.Equal(t, 1, code)
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	require.Len(t, lines, 2, stdout)
	assert.True(t, strings.HasPrefix(lines[0], "shoutrrr[0] (generic): "), lines[0])
	assert.NotEqual(t, "shoutrrr[0] (generic): ok", lines[0])
	assert.Equal(t, "shoutrrr[1] (generic): ok", lines[1])
	assert.Equal(t, int32(1), workingRequests.Load())
	assert.NotContains(t, stdout+stderr, secret)
}

func TestNotificationTestCommand_NoDestinationExitsNonZero(t *testing.T) {
	isolateCLI(t)

	code, _, stderr := runCLI(t, "notification", "test")

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "no notification destination")
}

func TestNotificationTestCommand_RejectsInvalidURLFromConfigFile(t *testing.T) {
	isolateCLI(t)
	t.Setenv("SEASONPACKARR__DISABLE_CONFIG_FILE", "")
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(`
notifications:
  shoutrrr: ["gotify://gotify.example/sentinel-invalid-token"]
`), 0o600))

	code, stdout, stderr := runCLI(t, "notification", "test", "--config", dir)

	assert.Equal(t, 1, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "notifications.shoutrrr[0] (gotify)")
	assert.NotContains(t, stderr, "sentinel-invalid-token")
}

func TestNotificationTestCommand_MissingConfigFileIsAnError(t *testing.T) {
	isolateCLI(t)
	t.Setenv("SEASONPACKARR__DISABLE_CONFIG_FILE", "")
	dir := t.TempDir()

	code, _, stderr := runCLI(t, "notification", "test", "--config", dir)

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "config.yaml")
	assert.NoFileExists(t, filepath.Join(dir, "config.yaml"), "the command must not write a config")
}
