// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/autobrr/go-deluge"
	"github.com/nuxencs/seasonpackarr/internal/domain"
)

type delugeLiveAPI interface {
	Close() error
	DaemonVersion(context.Context) (string, error)
	EnablePlugin(context.Context, string) error
	GetEnabledPlugins(context.Context) ([]string, error)
	RemoveTorrent(context.Context, string, bool) (bool, error)
}

type delugeLiveLabelAPI interface {
	GetTorrentLabel(string) (string, error)
}

// TestDelugeImportLive drives the adapter against a real native-RPC daemon.
// It is environment-gated and is not part of the CI workflow.
func TestDelugeImportLive(t *testing.T) {
	clientType := os.Getenv("SEASONPACKARR_TEST_DELUGE_TYPE")
	importDir := os.Getenv("SEASONPACKARR_TEST_IMPORT_DIR")
	if clientType == "" || importDir == "" {
		t.Skip("Deluge live-test environment is not set")
	}

	port := 58846
	if value := os.Getenv("SEASONPACKARR_TEST_DELUGE_PORT"); value != "" {
		parsedPort, parseErr := strconv.Atoi(value)
		if parseErr != nil {
			t.Fatalf("parse SEASONPACKARR_TEST_DELUGE_PORT: %v", parseErr)
		}
		port = parsedPort
	}
	c, err := newDelugeClient(&domain.Client{
		Type:     clientType,
		Host:     envOrDefault("SEASONPACKARR_TEST_DELUGE_HOST", "127.0.0.1"),
		Port:     port,
		Username: envOrDefault("SEASONPACKARR_TEST_DELUGE_USER", "seasonpackarr"),
		Password: envOrDefault("SEASONPACKARR_TEST_DELUGE_PASS", "integration"),
		Import: domain.ImportPolicy{
			SavePath: importDir,
			Tags:     []string{"SeasonPackArr"},
		},
	})
	if err != nil {
		t.Fatalf("connect to %s: %v", clientType, err)
	}
	c.pollInterval = 25 * time.Millisecond

	raw, ok := c.c.(delugeLiveAPI)
	if !ok {
		t.Fatal("Deluge client does not expose live-test operations")
	}
	t.Cleanup(func() {
		if err := raw.Close(); err != nil {
			t.Errorf("close Deluge client: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), delugeTimeout)
	defer cancel()
	version, err := raw.DaemonVersion(ctx)
	if err != nil {
		t.Fatalf("read daemon version: %v", err)
	}
	if clientType == "deluge-v1" && !strings.HasPrefix(version, "1.") {
		t.Fatalf("client type %s connected to daemon version %s", clientType, version)
	}
	if clientType == "deluge-v2" && !strings.HasPrefix(version, "2.") {
		t.Fatalf("client type %s connected to daemon version %s", clientType, version)
	}
	t.Logf("connected with %s to Deluge %s", clientType, version)

	if err := raw.EnablePlugin(ctx, "Label"); err != nil {
		t.Fatalf("enable Label plugin: %v", err)
	}
	enabled, err := raw.GetEnabledPlugins(ctx)
	if err != nil {
		t.Fatalf("get enabled plugins: %v", err)
	}
	if !containsString(enabled, "Label") {
		t.Fatalf("Label plugin was not enabled: %v", enabled)
	}

	t.Run("complete import, paths, and label", func(t *testing.T) {
		packName := fmt.Sprintf("LiveDeluge%s.S01.1080p.WEB-DL.H.264-RlsGrp", strings.TrimPrefix(clientType, "deluge-"))
		_, torrentBytes, hashes := buildLivePack(t, importDir, packName, 3)
		req := ImportRequest{TorrentBytes: torrentBytes, LegacyHash: hashes.Legacy, V2Hash: hashes.V2, HasV1: hashes.HasV1, SavePath: importDir}
		importAndRegisterCleanup(t, c, raw, req)

		status := requireDelugeStarted(t, c, hashes.Legacy)
		if status.Progress != 100 || status.TotalDone != status.TotalSize {
			t.Fatalf("complete import progress=%.2f totalDone=%d totalSize=%d", status.Progress, status.TotalDone, status.TotalSize)
		}

		torrents, err := c.GetTorrents()
		if err != nil {
			t.Fatalf("list torrents: %v", err)
		}
		if len(torrents) != 1 || torrents[0].SavePath != filepath.Clean(importDir) {
			t.Fatalf("unexpected torrents: %+v", torrents)
		}
		files, err := c.GetFiles(hashes.Legacy)
		if err != nil {
			t.Fatalf("list files: %v", err)
		}
		if len(files) != 3 || !strings.HasPrefix(files[0].Name, packName+"/") {
			t.Fatalf("unexpected files: %+v", files)
		}
		requireDelugeLabel(t, c, hashes.Legacy, "seasonpackarr")
	})

	t.Run("partial import checks present data and resumes", func(t *testing.T) {
		packName := fmt.Sprintf("PartialDeluge%s.S01.1080p.WEB-DL.H.264-RlsGrp", strings.TrimPrefix(clientType, "deluge-"))
		_, torrentBytes, hashes := buildPartialPack(t, importDir, packName, 3)
		req := ImportRequest{TorrentBytes: torrentBytes, LegacyHash: hashes.Legacy, V2Hash: hashes.V2, HasV1: hashes.HasV1, SavePath: importDir}
		importAndRegisterCleanup(t, c, raw, req)

		status := requireDelugeStarted(t, c, hashes.Legacy)
		if status.Progress <= 0 || status.Progress >= 100 {
			t.Fatalf("partial import progress=%.2f, want between 0 and 100", status.Progress)
		}
		if status.TotalDone <= 0 || status.TotalDone >= status.TotalSize {
			t.Fatalf("partial import totalDone=%d totalSize=%d", status.TotalDone, status.TotalSize)
		}
		requireDelugeLabel(t, c, hashes.Legacy, "seasonpackarr")
	})
}

func importAndRegisterCleanup(t *testing.T, c *delugeClient, raw delugeLiveAPI, req ImportRequest) {
	t.Helper()
	if err := c.Import(req); err != nil {
		t.Fatalf("import: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), delugeTimeout)
		defer cancel()
		if _, err := raw.RemoveTorrent(ctx, req.LegacyHash, false); err != nil {
			t.Errorf("remove torrent: %v", err)
		}
	})
}

func requireDelugeStarted(t *testing.T, c *delugeClient, hash string) *deluge.TorrentStatus {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var status *deluge.TorrentStatus
	for ctx.Err() == nil {
		c.mu.Lock()
		var err error
		status, err = c.c.TorrentStatus(ctx, hash)
		c.mu.Unlock()
		if err != nil {
			t.Fatalf("read torrent status: %v", err)
		}
		if status != nil && deluge.TorrentState(status.State) != deluge.StatePaused && deluge.TorrentState(status.State) != deluge.StateChecking {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status == nil {
		t.Fatal("torrent status is nil")
	}
	if deluge.TorrentState(status.State) == deluge.StatePaused {
		t.Fatalf("torrent remained paused: %+v", status)
	}
	if deluge.TorrentState(status.State) == deluge.StateError {
		t.Fatalf("torrent entered error state: %+v", status)
	}
	return status
}

func requireDelugeLabel(t *testing.T, c *delugeClient, hash, want string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), delugeTimeout)
	defer cancel()
	c.mu.Lock()
	plugin, err := c.label(ctx)
	c.mu.Unlock()
	if err != nil || plugin == nil {
		t.Fatalf("load Label plugin: plugin=%v err=%v", plugin, err)
	}
	reader, ok := plugin.(delugeLiveLabelAPI)
	if !ok {
		t.Fatal("Label plugin does not expose label reads")
	}
	c.mu.Lock()
	got, err := reader.GetTorrentLabel(hash)
	c.mu.Unlock()
	if err != nil {
		t.Fatalf("get torrent label: %v", err)
	}
	if got != want {
		t.Fatalf("label=%q want=%q", got, want)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
