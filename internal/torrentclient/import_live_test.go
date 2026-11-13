// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/hekmon/transmissionrpc/v3"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/torrents"
)

// torrentBytesFromFolder builds a valid v1 torrent for the flat test pack.
// It includes piece hashes so a real client can verify the files on disk.
func torrentBytesFromFolder(t *testing.T, folderPath string) []byte {
	t.Helper()

	const pieceLength = 256 * 1024
	info := metainfo.Info{
		Name:        filepath.Base(folderPath),
		PieceLength: pieceLength,
	}
	var content []byte

	entries, err := os.ReadDir(folderPath)
	if err != nil {
		t.Fatalf("read pack directory: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(folderPath, entry.Name()))
		if err != nil {
			t.Fatalf("read pack file: %v", err)
		}
		info.Files = append(info.Files, metainfo.FileInfo{
			Path:   []string{entry.Name()},
			Length: int64(len(data)),
		})
		content = append(content, data...)
	}

	for offset := 0; offset < len(content); offset += pieceLength {
		end := min(offset+pieceLength, len(content))
		hash := sha1.Sum(content[offset:end])
		info.Pieces = append(info.Pieces, hash[:]...)
	}

	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("encode torrent info: %v", err)
	}
	var torrentBytes bytes.Buffer
	if err := (&metainfo.MetaInfo{InfoBytes: infoBytes}).Write(&torrentBytes); err != nil {
		t.Fatalf("encode torrent: %v", err)
	}
	return torrentBytes.Bytes()
}

// buildLivePack writes a complete season pack under importDir and returns the
// pack name, its .torrent bytes, and its info hashes. The data is left on disk so
// the client can verify it as an already-present import.
func buildLivePack(t *testing.T, importDir, packName string, episodes int) (string, []byte, torrents.Hashes) {
	t.Helper()

	packDir := filepath.Join(importDir, packName)
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatalf("mkdir pack dir: %v", err)
	}
	for i := 1; i <= episodes; i++ {
		ep := fmt.Sprintf("%s.mkv", strings.Replace(packName, ".S01.", fmt.Sprintf(".S01E%02d.", i), 1))
		if err := os.WriteFile(filepath.Join(packDir, ep), []byte("0"), 0o644); err != nil {
			t.Fatalf("write episode: %v", err)
		}
	}

	torrentBytes := torrentBytesFromFolder(t, packDir)
	hashes, err := torrents.InfoHashes(torrentBytes)
	if err != nil {
		t.Fatalf("InfoHashes: %v", err)
	}
	return packName, torrentBytes, hashes
}

// requireQbitStarted polls until the torrent is active and asserts a correct
// import always ends up started (never left stopped).
func requireQbitStarted(t *testing.T, c *qbitClient, hash string) {
	t.Helper()
	var final qbittorrent.Torrent
	for range 20 {
		tor, ok, err := c.lookupTorrent(hash)
		if err != nil || !ok {
			t.Fatalf("lookup after import: ok=%v err=%v", ok, err)
		}
		final = tor
		if isActiveTorrentState(final.State) {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Logf("FINAL qbit state=%s progress=%.2f", final.State, final.Progress)
	if !isActiveTorrentState(final.State) {
		t.Errorf("import left the torrent stopped (state=%s) - a correct import must always start", final.State)
	}
}

// requireTransmissionStarted asserts the imported torrent is not left stopped
// and has no error.
func requireTransmissionStarted(t *testing.T, c *transmissionClient, hash string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), transmissionTimeout)
	defer cancel()
	ts, err := c.c.TorrentGetHashes(ctx, []string{"status", "percentDone", "errorString"}, []string{hash})
	if err != nil || len(ts) == 0 {
		t.Fatalf("get after import: err=%v n=%d", err, len(ts))
	}
	tr := ts[0]
	var status transmissionrpc.TorrentStatus
	if tr.Status != nil {
		status = *tr.Status
	}
	pd := 0.0
	if tr.PercentDone != nil {
		pd = *tr.PercentDone
	}
	t.Logf("FINAL transmission status=%d percentDone=%.2f error=%q", status, pd, derefString(tr.ErrorString))
	if status == transmissionrpc.TorrentStatusStopped {
		t.Errorf("import left the torrent stopped - a correct import must always start")
	}
	if es := derefString(tr.ErrorString); es != "" {
		t.Errorf("transmission reported error after import: %q", es)
	}
}

// requireFileReadLoad exercises the adapter's multi-hash read shape against a
// live client. Repeating one known hash isolates read batching and concurrency
// from torrent setup cost. The missing hash verifies partial-result handling.
func requireFileReadLoad(t *testing.T, client TorrentClient, hash string) {
	t.Helper()

	const readCount = 24
	hashes := make([]string, readCount+1)
	for index := range readCount {
		hashes[index] = hash
	}
	hashes[readCount] = strings.Repeat("0", 40)

	started := time.Now()
	results := client.GetFiles(hashes)
	duration := time.Since(started)
	if len(results) != len(hashes) {
		t.Fatalf("GetFiles returned %d results, want %d", len(results), len(hashes))
	}
	for index := range readCount {
		if results[index].Hash != hash || results[index].Err != nil || len(results[index].Files) == 0 {
			t.Fatalf("GetFiles result %d = %+v, want successful hash %s", index, results[index], hash)
		}
	}
	if results[readCount].Err == nil {
		t.Fatalf("GetFiles missing-hash result = %+v, want an error", results[readCount])
	}
	t.Logf("GetFiles returned %d successful file lists plus one missing result in %s", readCount, duration)
}

// TestQbitImportLive drives qbitClient.Import against a real qBittorrent.
//
//	SEASONPACKARR_TEST_QBIT_HOST=127.0.0.1:8080 \
//	SEASONPACKARR_TEST_QBIT_USER=admin SEASONPACKARR_TEST_QBIT_PASS=... \
//	SEASONPACKARR_TEST_IMPORT_DIR=/shared/downloads go test -run Live ./internal/torrentclient/
func TestQbitImportLive(t *testing.T) {
	host := os.Getenv("SEASONPACKARR_TEST_QBIT_HOST")
	importDir := os.Getenv("SEASONPACKARR_TEST_IMPORT_DIR")
	if host == "" || importDir == "" {
		t.Skip("SEASONPACKARR_TEST_QBIT_HOST / SEASONPACKARR_TEST_IMPORT_DIR not set - skipping live import test")
	}

	policy := domain.ImportPolicy{SavePath: importDir, Tags: []string{"seasonpackarr"}}
	c, err := newQbitClient(&domain.Client{
		Host:     host,
		Username: os.Getenv("SEASONPACKARR_TEST_QBIT_USER"),
		Password: os.Getenv("SEASONPACKARR_TEST_QBIT_PASS"),
		Import:   policy,
	})
	if err != nil {
		t.Fatalf("newQbitClient: %v", err)
	}

	packName, torrentBytes, hashes := buildLivePack(t, importDir, "LiveQbit.S01.1080p.WEB-DL.H.264-RlsGrp", 3)
	t.Logf("importing pack %q hash=%s", packName, hashes.Legacy)

	if err := c.Import(ImportRequest{TorrentBytes: torrentBytes, LegacyHash: hashes.Legacy, V2Hash: hashes.V2, HasV1: hashes.HasV1, SavePath: importDir}); err != nil {
		t.Fatalf("qbit Import: %v", err)
	}
	requireQbitStarted(t, c, hashes.Legacy)
	requireFileReadLoad(t, c, hashes.Legacy)
}

// TestQbitImportDestinationPreferencesLive verifies the category-only path
// matrix against a real qBittorrent preferences and categories API.
func TestQbitImportDestinationPreferencesLive(t *testing.T) {
	host := os.Getenv("SEASONPACKARR_TEST_QBIT_HOST")
	importDir := os.Getenv("SEASONPACKARR_TEST_IMPORT_DIR")
	if host == "" || importDir == "" {
		t.Skip("SEASONPACKARR_TEST_QBIT_HOST / SEASONPACKARR_TEST_IMPORT_DIR not set - skipping live destination test")
	}

	categoryName := fmt.Sprintf("seasonpackarr-live-%d", time.Now().UnixNano())
	categoryPath := filepath.Join(importDir, "category")
	c, err := newQbitClient(&domain.Client{
		Host:     host,
		Username: os.Getenv("SEASONPACKARR_TEST_QBIT_USER"),
		Password: os.Getenv("SEASONPACKARR_TEST_QBIT_PASS"),
		Import: domain.ImportPolicy{
			Category:      categoryName,
			ContentLayout: "subfolder",
		},
	})
	if err != nil {
		t.Fatalf("newQbitClient: %v", err)
	}

	raw := c.c.(*qbittorrent.Client)
	original, err := raw.GetAppPreferences()
	if err != nil {
		t.Fatalf("get original preferences: %v", err)
	}
	t.Cleanup(func() {
		if err := raw.SetPreferences(map[string]any{
			"auto_tmm_enabled":                  original.AutoTmmEnabled,
			"use_category_paths_in_manual_mode": original.UseCategoryPathsInManualMode,
			"save_path":                         original.SavePath,
		}); err != nil {
			t.Errorf("restore preferences: %v", err)
		}
		if err := raw.RemoveCategories([]string{categoryName}); err != nil {
			t.Errorf("remove category: %v", err)
		}
	})

	if err := raw.SetPreferences(map[string]any{"save_path": importDir}); err != nil {
		t.Fatalf("set default save path: %v", err)
	}
	if err := raw.CreateCategory(categoryName, categoryPath); err != nil {
		t.Fatalf("create category: %v", err)
	}

	tests := []struct {
		name               string
		autoTMM            bool
		manualCategoryPath bool
		want               string
	}{
		{name: "automatic management", autoTMM: true, want: categoryPath},
		{name: "manual global path", want: importDir},
		{name: "manual category path", manualCategoryPath: true, want: categoryPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := raw.SetPreferences(map[string]any{
				"auto_tmm_enabled":                  tt.autoTMM,
				"use_category_paths_in_manual_mode": tt.manualCategoryPath,
			}); err != nil {
				t.Fatalf("set preferences: %v", err)
			}

			destination, err := c.ImportDestination()
			if err != nil {
				t.Fatalf("ImportDestination: %v", err)
			}
			if got := destination.SavePath(); got != normalizePath(tt.want) {
				t.Fatalf("destination=%q want=%q", got, normalizePath(tt.want))
			}
		})
	}
}

// TestTransmissionImportLive drives transmissionClient.Import against a real
// Transmission (add paused -> verify -> poll -> start).
func TestTransmissionImportLive(t *testing.T) {
	host := os.Getenv("SEASONPACKARR_TEST_TRANSMISSION_HOST")
	importDir := os.Getenv("SEASONPACKARR_TEST_IMPORT_DIR")
	if host == "" || importDir == "" {
		t.Skip("SEASONPACKARR_TEST_TRANSMISSION_HOST / SEASONPACKARR_TEST_IMPORT_DIR not set - skipping live import test")
	}

	policy := domain.ImportPolicy{SavePath: importDir, Tags: []string{"seasonpackarr"}}
	c, err := newTransmissionClient(&domain.Client{
		Host:     host,
		Username: os.Getenv("SEASONPACKARR_TEST_TRANSMISSION_USER"),
		Password: os.Getenv("SEASONPACKARR_TEST_TRANSMISSION_PASS"),
		Import:   policy,
	})
	if err != nil {
		t.Fatalf("newTransmissionClient: %v", err)
	}

	packName, torrentBytes, hashes := buildLivePack(t, importDir, "LiveTransmission.S01.1080p.WEB-DL.H.264-RlsGrp", 3)
	t.Logf("importing pack %q hash=%s", packName, hashes.Legacy)

	if err := c.Import(ImportRequest{TorrentBytes: torrentBytes, LegacyHash: hashes.Legacy, V2Hash: hashes.V2, HasV1: hashes.HasV1, SavePath: importDir}); err != nil {
		t.Fatalf("transmission Import: %v", err)
	}
	requireTransmissionStarted(t, c, hashes.Legacy)
	requireFileReadLoad(t, c, hashes.Legacy)
}
