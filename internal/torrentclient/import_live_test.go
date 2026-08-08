// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/autobrr/go-qbittorrent"
	"github.com/hekmon/transmissionrpc/v3"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/torrents"
)

// torrentBytesFromFolder builds an in-memory .torrent for an on-disk folder
// without touching its files, so the pack data can stay in place for the client
// to verify as an already-present import.
func torrentBytesFromFolder(t *testing.T, folderPath string) []byte {
	t.Helper()
	info := metainfo.Info{PieceLength: 256 * 1024}
	if err := info.BuildFromFilePath(folderPath); err != nil {
		t.Fatalf("BuildFromFilePath: %v", err)
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("bencode info: %v", err)
	}
	var buf bytes.Buffer
	if err := (&metainfo.MetaInfo{InfoBytes: infoBytes}).Write(&buf); err != nil {
		t.Fatalf("write metainfo: %v", err)
	}
	return buf.Bytes()
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
		t.Errorf("import left the torrent stopped (state=%s) — a correct import must always start", final.State)
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
		t.Errorf("import left the torrent stopped — a correct import must always start")
	}
	if es := derefString(tr.ErrorString); es != "" {
		t.Errorf("transmission reported error after import: %q", es)
	}
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
		t.Skip("SEASONPACKARR_TEST_QBIT_HOST / SEASONPACKARR_TEST_IMPORT_DIR not set — skipping live import test")
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
}

// TestTransmissionImportLive drives transmissionClient.Import against a real
// Transmission (add paused -> verify -> poll -> start).
func TestTransmissionImportLive(t *testing.T) {
	host := os.Getenv("SEASONPACKARR_TEST_TRANSMISSION_HOST")
	importDir := os.Getenv("SEASONPACKARR_TEST_IMPORT_DIR")
	if host == "" || importDir == "" {
		t.Skip("SEASONPACKARR_TEST_TRANSMISSION_HOST / SEASONPACKARR_TEST_IMPORT_DIR not set — skipping live import test")
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
}
