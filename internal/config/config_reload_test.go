// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/logger/loggertest"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const beforeConfig = `
clients:
  default:
    type: qbittorrent
    import:
      category: before
`

const afterConfig = `
clients:
  default:
    type: qbittorrent
    import:
      category: after
`

// reloadLoop runs reloadOnChange of cfg in a synctest bubble. Its last party counts reloads in a buffer that is larger
// than any test needs, so no reload is lost.
type reloadLoop struct {
	changes  chan struct{}
	reloaded chan struct{}
	stop     context.CancelFunc
	done     chan struct{}
}

func startReloadLoop(t *testing.T, cfg *AppConfig, log *loggertest.Logger, parties ...func(domain.Config)) *reloadLoop {
	t.Helper()
	ctx, stop := context.WithCancel(t.Context())
	l := &reloadLoop{changes: make(chan struct{}), reloaded: make(chan struct{}, 16), stop: stop, done: make(chan struct{})}
	parties = append(parties, func(domain.Config) { l.reloaded <- struct{}{} })
	go func() {
		defer close(l.done)
		cfg.reloadOnChange(ctx, log, l.changes, parties)
	}()
	return l
}

// changed sends the "file changed" signal that the file watcher sends.
func (l *reloadLoop) changed() { l.changes <- struct{}{} }

// reloads waits until the loop is idle and returns the number of accepted reloads since the last call.
func (l *reloadLoop) reloads() int {
	synctest.Wait()
	n := len(l.reloaded)
	for range n {
		<-l.reloaded
	}
	return n
}

func TestReloadOnChange_StartsNoReloadAfterStopWithPendingTimer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg, configFile := newTestAppConfig(t, beforeConfig)
		log := loggertest.New()
		loop := startReloadLoop(t, cfg, log)

		writeConfigFile(t, configFile, afterConfig)
		loop.changed()
		time.Sleep(configReloadDebounceDelay / 2)
		loop.stop()
		time.Sleep(2 * configReloadDebounceDelay)

		synctest.Wait()
		select {
		case <-loop.done:
		default:
			require.FailNow(t, "the reload loop did not return after the stop")
		}
		assert.Equal(t, 0, loop.reloads())
		assert.Equal(t, "before", cfg.Snapshot().Clients["default"].Import.Category)
		assert.Empty(t, log.String())
	})
}

func TestReloadOnChange_DebouncesTruncateFirstWrites(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg, configFile := newTestAppConfig(t, beforeConfig)
		log := loggertest.New()
		loop := startReloadLoop(t, cfg, log)

		writeConfigFile(t, configFile, "")
		loop.changed()
		time.Sleep(configReloadDebounceDelay / 4)
		require.Equal(t, 0, loop.reloads())

		writeConfigFile(t, configFile, afterConfig)
		loop.changed()
		time.Sleep(configReloadDebounceDelay / 4)
		require.Equal(t, 0, loop.reloads())

		writeConfigFile(t, configFile, afterConfig+"\n")
		loop.changed()
		time.Sleep(configReloadDebounceDelay)
		assert.Equal(t, 1, loop.reloads())
		time.Sleep(2 * configReloadDebounceDelay)
		assert.Equal(t, 0, loop.reloads())

		assert.NotContains(t, log.String(), "config reload rejected")
		assert.Equal(t, "after", cfg.Snapshot().Clients["default"].Import.Category)
	})
}

func TestReloadOnChange_ReloadsOnceAfterEventBurst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg, configFile := newTestAppConfig(t, beforeConfig)
		log := loggertest.New()
		loop := startReloadLoop(t, cfg, log)

		writeConfigFile(t, configFile, afterConfig)
		for range 10 {
			loop.changed()
			time.Sleep(configReloadDebounceDelay / 2)
			require.Equal(t, 0, loop.reloads(), "a reload started before the burst became quiet")
		}
		time.Sleep(configReloadDebounceDelay / 2)
		assert.Equal(t, 1, loop.reloads())
		time.Sleep(2 * configReloadDebounceDelay)
		assert.Equal(t, 0, loop.reloads())
		assert.Equal(t, "after", cfg.Snapshot().Clients["default"].Import.Category)
	})
}

func TestReloadOnChange_ReloadsAgainAfterNextChange(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg, configFile := newTestAppConfig(t, beforeConfig)
		loop := startReloadLoop(t, cfg, loggertest.New())

		writeConfigFile(t, configFile, afterConfig)
		loop.changed()
		time.Sleep(configReloadDebounceDelay)
		require.Equal(t, 1, loop.reloads())

		writeConfigFile(t, configFile, beforeConfig)
		loop.changed()
		time.Sleep(configReloadDebounceDelay)
		assert.Equal(t, 1, loop.reloads())
		assert.Equal(t, "before", cfg.Snapshot().Clients["default"].Import.Category)
	})
}

// TestReloadOnChange_KeepsSnapshotWhileConfigWriteIsIncomplete: a reload that reads the empty file of a write in
// progress is rejected, and the completed write is the next reload.
func TestReloadOnChange_KeepsSnapshotWhileConfigWriteIsIncomplete(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg, configFile := newTestAppConfig(t, beforeConfig)
		log := loggertest.New()
		loop := startReloadLoop(t, cfg, log)

		writeConfigFile(t, configFile, "")
		loop.changed()
		time.Sleep(configReloadDebounceDelay)
		require.Equal(t, 0, loop.reloads())
		assert.Contains(t, log.String(), "config reload rejected; keeping previous config")
		assert.Equal(t, "before", cfg.Snapshot().Clients["default"].Import.Category)

		writeConfigFile(t, configFile, afterConfig)
		loop.changed()
		time.Sleep(configReloadDebounceDelay)
		assert.Equal(t, 1, loop.reloads())
		assert.Equal(t, "after", cfg.Snapshot().Clients["default"].Import.Category)
	})
}

// configSecrets are the sentinel secrets of configWithSecrets by kind.
var configSecrets = map[string]string{
	"apiToken":        "sentinel-config-api-token",
	"client password": "sentinel-config-client-password",
	"Prowlarr apiKey": "sentinel-config-prowlarr-api-key",
	"Discord webhook": "sentinel-config-discord-webhook-token",
}

// configWithSecrets returns a valid config that holds every secret kind.
// replace changes it into an invalid config near the secrets.
func configWithSecrets(replace ...string) string {
	return strings.NewReplacer(replace...).Replace(`apiToken: sentinel-config-api-token
clients:
  default:
    type: qbittorrent
    host: http://qbittorrent:8080
    port: 8080
    username: admin
    password: sentinel-config-client-password
    import:
      category: tv-hd
search:
  prowlarrURL: http://prowlarr:9696
  apiKey: sentinel-config-prowlarr-api-key
notifications:
  discord: https://discord.com/api/webhooks/123/sentinel-config-discord-webhook-token
`)
}

func TestReloadOnChange_KeepsSnapshotAndLogsNoSecretOnRejectedReload(t *testing.T) {
	tests := map[string][]string{
		"YAML syntax error after the apiToken": {"clients:\n", "clients: [\n"},
		"YAML indentation error at the password": {
			"    password: sentinel-config-client-password\n", "     password: sentinel-config-client-password\n",
		},
		"port next to the password is not a number": {"port: 8080", "port: not-a-port"},
		"password is a map":                         {"password: sentinel-config-client-password", "password: {value: sentinel-config-client-password}"},
		"apiToken is a list":                        {"apiToken: sentinel-config-api-token", "apiToken: [sentinel-config-api-token]"},
		"Prowlarr apiKey is a list":                 {"apiKey: sentinel-config-prowlarr-api-key", "apiKey: [sentinel-config-prowlarr-api-key]"},
		"Discord webhook is a list": {
			"discord: https://discord.com/api/webhooks/123/sentinel-config-discord-webhook-token",
			"discord: [https://discord.com/api/webhooks/123/sentinel-config-discord-webhook-token]",
		},
		"unknown key next to the password": {"    username: admin\n", "    username: admin\n    passwrd: sentinel-config-client-password\n"},
		"client validation fails":          {"type: qbittorrent", "type: transmission"},
		"Prowlarr URL holds credentials": {
			"prowlarrURL: http://prowlarr:9696", "prowlarrURL: http://admin:sentinel-config-prowlarr-api-key@prowlarr:9696",
		},
	}
	for name, replace := range tests {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				invalid := configWithSecrets(replace...)
				require.NotEqual(t, configWithSecrets(), invalid, "the replacement matched nothing")
				cfg, configFile := newTestAppConfig(t, configWithSecrets())
				before := cfg.Snapshot()
				log := loggertest.New()
				loop := startReloadLoop(t, cfg, log)

				writeConfigFile(t, configFile, invalid)
				loop.changed()
				time.Sleep(configReloadDebounceDelay)

				assert.Equal(t, 0, loop.reloads())
				assert.Equal(t, before, cfg.Snapshot())
				// The reload must be rejected and logged, or the secret check proves nothing.
				require.Contains(t, log.String(), "config reload rejected; keeping previous config")
				log.AssertNoSecrets(t, configSecrets)
			})
		})
	}
}

func TestReloadOnChange_CallsPartiesInOrderWithOwnSnapshot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg, configFile := newTestAppConfig(t, beforeConfig)
		var calls []string
		first := func(snapshot domain.Config) {
			calls = append(calls, "first:"+snapshot.Clients["default"].Import.Category)
			snapshot.Clients["default"].Import.Category = "changed by first"
			snapshot.LogLevel = "TRACE"
		}
		second := func(snapshot domain.Config) {
			calls = append(calls, "second:"+snapshot.Clients["default"].Import.Category+":"+snapshot.LogLevel)
		}
		loop := startReloadLoop(t, cfg, loggertest.New(), first, second)

		writeConfigFile(t, configFile, afterConfig)
		loop.changed()
		time.Sleep(configReloadDebounceDelay)

		require.Equal(t, 1, loop.reloads())
		assert.Equal(t, []string{"first:after", "second:after:DEBUG"}, calls)
		assert.Equal(t, "after", cfg.Snapshot().Clients["default"].Import.Category)
	})
}

func TestWatch_ReturnsWatcherSetUpError(t *testing.T) {
	cfg, configFile := newTestAppConfig(t, beforeConfig)
	require.NoError(t, os.Remove(configFile))

	err := cfg.Watch(t.Context(), loggertest.New(), func(domain.Config) { t.Error("a party ran without a reload") })

	require.ErrorContains(t, err, "watching config file "+configFile)
}

func TestWatch_DisabledConfigFileStartsNothing(t *testing.T) {
	cfg, err := New(Input{ConfigDir: t.TempDir(), Environ: []string{"SEASONPACKARR__DISABLE_CONFIG_FILE=true"}, Version: "test"})
	require.NoError(t, err)

	// The config directory has no config.yaml, so a watch that started would fail to set up its watcher.
	err = cfg.Watch(t.Context(), loggertest.New(), func(domain.Config) { t.Error("a party ran without a config file") })

	require.NoError(t, err)
}

// watchReloads starts cfg.Watch with one party that signals each accepted reload on the returned channel.
func watchReloads(t *testing.T, cfg *AppConfig, log logger.Logger) <-chan struct{} {
	t.Helper()
	reloads := make(chan struct{}, 16)
	require.NoError(t, cfg.Watch(t.Context(), log, func(domain.Config) { reloads <- struct{}{} }))
	return reloads
}

// requireReload fails the test when no reload of a real file system event arrives in time.
func requireReload(t *testing.T, reloads <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-reloads:
	case <-time.After(3 * time.Second):
		require.FailNow(t, "timed out waiting for "+msg)
	}
}

func TestWatch_PublishesSnapshotForConcurrentReaders(t *testing.T) {
	previousLogLevel := zerolog.GlobalLevel()
	t.Cleanup(func() {
		zerolog.SetGlobalLevel(previousLogLevel)
	})

	cfg, configFile := newTestAppConfig(t, beforeConfig)
	snapshot := cfg.Snapshot()
	reloads := watchReloads(t, cfg, logger.New(&snapshot))

	readerResult := make(chan error, 1)
	go func() {
		for {
			snapshot := cfg.Snapshot()
			client, ok := snapshot.Clients["default"]
			if !ok || client == nil {
				readerResult <- fmt.Errorf("published snapshot has no default client")
				return
			}
			if client.Import.Category == "after" {
				readerResult <- nil
				return
			}
		}
	}()

	writeConfigFile(t, configFile, afterConfig+"logLevel: INFO\n")
	requireReload(t, reloads, "config reload")
	select {
	case err := <-readerResult:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		require.FailNow(t, "concurrent snapshot reader did not observe reload")
	}
	require.Equal(t, "after", cfg.Snapshot().Clients["default"].Import.Category)
}

func TestWatch_SurvivesRenameAndRecreateSave(t *testing.T) {
	cfg, configFile := newTestAppConfig(t, beforeConfig)
	log := loggertest.New()
	reloads := watchReloads(t, cfg, log)

	require.NoError(t, os.Rename(configFile, configFile+".backup"))
	require.Eventually(t, func() bool {
		return strings.Contains(log.String(), "config reload rejected; keeping previous config")
	}, 3*time.Second, 10*time.Millisecond, log.String())

	writeConfigFile(t, configFile, strings.Replace(beforeConfig, "before", "after-first-save", 1))
	requireReload(t, reloads, "reload after rename-and-recreate save")
	require.Equal(t, "after-first-save", cfg.Snapshot().Clients["default"].Import.Category)

	writeConfigFile(t, configFile, strings.Replace(beforeConfig, "before", "after-second-save", 1))
	requireReload(t, reloads, "reload after the next save")
	require.Equal(t, "after-second-save", cfg.Snapshot().Clients["default"].Import.Category)
	require.NotContains(t, log.String(), "error watching config file")
}

func TestWatch_FollowsReplacedConfigSymlink(t *testing.T) {
	configDir := t.TempDir()
	firstTarget := filepath.Join(configDir, "config-first.yaml")
	secondTarget := filepath.Join(configDir, "config-second.yaml")
	configFile := filepath.Join(configDir, "config.yaml")
	writeConfigFile(t, firstTarget, beforeConfig)
	writeConfigFile(t, secondTarget, afterConfig)
	if err := os.Symlink(firstTarget, configFile); err != nil {
		t.Skipf("config symlinks are unavailable: %v", err)
	}
	nextSymlink := configFile + ".next"
	require.NoError(t, os.Symlink(secondTarget, nextSymlink))

	cfg, err := New(Input{ConfigDir: configDir, Version: "test"})
	require.NoError(t, err)
	reloads := watchReloads(t, cfg, loggertest.New())

	require.NoError(t, os.Rename(nextSymlink, configFile))
	writeConfigFile(t, secondTarget, afterConfig)

	requireReload(t, reloads, "reload after config symlink replacement")
	require.Equal(t, "after", cfg.Snapshot().Clients["default"].Import.Category)
}
