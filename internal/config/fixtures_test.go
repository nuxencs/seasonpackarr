// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger/loggertest"

	"github.com/stretchr/testify/require"
)

// reloadRejected is the message of the log event of a rejected reload.
const reloadRejected = "config reload rejected; keeping previous config"

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte(content), 0o644))

	return configFile
}

// testInput returns the input of a load of a new config.yaml with contents, with environ as the whole environment.
func testInput(t *testing.T, contents string, environ ...string) Input {
	t.Helper()
	return Input{ConfigDir: filepath.Dir(writeTestConfig(t, contents)), Environ: environ, Version: "test"}
}

// loadFile loads configFile with environ as the whole environment.
func loadFile(configFile string, environ ...string) (domain.Config, error) {
	return Load(Input{ConfigDir: filepath.Dir(configFile), Environ: environ, Version: "test"})
}

// loadWithFile loads a new config.yaml with contents, with environ as the whole environment.
func loadWithFile(t *testing.T, contents string, environ ...string) (domain.Config, error) {
	t.Helper()
	return Load(testInput(t, contents, environ...))
}

// newTestAppConfig returns the holder of a new config.yaml with contents and the path of that file.
func newTestAppConfig(t *testing.T, contents string, environ ...string) (*AppConfig, string) {
	t.Helper()
	in := testInput(t, contents, environ...)
	cfg, err := New(in)
	require.NoError(t, err)
	return cfg, filepath.Join(in.ConfigDir, "config.yaml")
}

func writeConfigFile(t testing.TB, configFile, contents string) {
	t.Helper()
	require.NoError(t, os.WriteFile(configFile, []byte(contents), 0o644))
}

// reloadWithFile writes contents to configFile and sends one "file changed" signal to the reload loop of cfg.
// It returns the logged reason of a rejected reload, or "" when the reload was accepted.
func reloadWithFile(t *testing.T, cfg *AppConfig, configFile, contents string) string {
	t.Helper()
	writeConfigFile(t, configFile, contents)
	log := loggertest.New()
	accepted := 0
	synctest.Test(t, func(t *testing.T) {
		loop := startReloadLoop(t, cfg, log)
		loop.changed()
		time.Sleep(configReloadDebounceDelay)
		accepted = loop.reloads()
	})
	if accepted == 1 {
		return ""
	}
	reason, _ := log.Events(t).Require(t, reloadRejected)["error"].(string)
	require.NotEmpty(t, reason, "the rejected reload logged no reason")
	return reason
}
