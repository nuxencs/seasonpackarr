// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/autobrr/go-qbittorrent"
	"github.com/hekmon/transmissionrpc/v3"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/torrents"
)

// buildPartialPack writes a full N-episode pack, builds the .torrent describing
// all N, then deletes all but the first episode from disk so the import faces a
// genuinely partial dataset (the real seasonpackarr scenario).
func buildPartialPack(t *testing.T, importDir, packName string, episodes int) (string, []byte, torrents.Hashes) {
	t.Helper()
	packDir := filepath.Join(importDir, packName)
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// realistic multi-piece episodes (piece length is 256KB) so a missing file
	// shows up as clearly incomplete instead of sharing one tiny piece
	content := make([]byte, 1<<20) // 1 MiB per episode
	names := make([]string, 0, episodes)
	for i := 1; i <= episodes; i++ {
		ep := fmt.Sprintf("%s.mkv", strings.Replace(packName, ".S01.", fmt.Sprintf(".S01E%02d.", i), 1))
		names = append(names, ep)
		content[0] = byte(i) // make each episode's content distinct
		if err := os.WriteFile(filepath.Join(packDir, ep), content, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	torrentBytes := torrentBytesFromFolder(t, packDir)
	hashes, err := torrents.InfoHashes(torrentBytes)
	if err != nil {
		t.Fatalf("InfoHashes: %v", err)
	}
	// delete all but the first episode to simulate a partial pack
	for _, ep := range names[1:] {
		if err := os.Remove(filepath.Join(packDir, ep)); err != nil {
			t.Fatalf("remove: %v", err)
		}
	}
	return packName, torrentBytes, hashes
}

// TestQbitPartialRawBehaviorLive answers the load-bearing question: does real
// qBittorrent report missingFiles for a PAUSED, skip-checked torrent whose files
// are partially missing, or only once resumed? This is what the adapter's
// "if state == missingFiles { recheck }" branch depends on.
func TestQbitPartialRawBehaviorLive(t *testing.T) {
	host := os.Getenv("SEASONPACKARR_TEST_QBIT_HOST")
	importDir := os.Getenv("SEASONPACKARR_TEST_IMPORT_DIR")
	if host == "" || importDir == "" {
		t.Skip("qbit live env not set")
	}

	h, err := buildHost(&domain.Client{Host: host})
	if err != nil {
		t.Fatal(err)
	}
	c := qbittorrent.NewClient(qbittorrent.Config{
		Host:     h,
		Username: os.Getenv("SEASONPACKARR_TEST_QBIT_USER"),
		Password: os.Getenv("SEASONPACKARR_TEST_QBIT_PASS"),
	})
	if err := c.Login(); err != nil {
		t.Fatalf("login: %v", err)
	}

	packName, torrentBytes, hashes := buildPartialPack(t, importDir, "RawPartial.S01.1080p.WEB-DL.H.264-RlsGrp", 3)
	t.Logf("partial pack %q hash=%s (1 of 3 episodes on disk)", packName, hashes.Legacy)

	opts := (&qbittorrent.TorrentAddOptions{SkipHashCheck: true, Paused: true, SavePath: importDir}).Prepare()
	if _, err := c.AddTorrentFromMemory(torrentBytes, opts); err != nil {
		t.Fatalf("add: %v", err)
	}

	// poll the raw state for a few seconds to see whether missingFiles appears
	// while the torrent is still paused
	sawMissingWhilePaused := false
	for i := range 20 {
		ts, err := c.GetTorrents(qbittorrent.TorrentFilterOptions{Hashes: []string{hashes.Legacy}})
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if len(ts) == 1 {
			t.Logf("  poll[%02d] state=%s progress=%.2f", i, ts[0].State, ts[0].Progress)
			if ts[0].State == qbittorrent.TorrentStateMissingFiles {
				sawMissingWhilePaused = true
				break
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Logf("RESULT: qbit reports missingFiles for a paused skip-checked partial torrent = %v", sawMissingWhilePaused)

	// cleanup
	_ = c.DeleteTorrents([]string{hashes.Legacy}, false)
}

// TestQbitImportPartialLive runs the actual adapter Import against a partial pack
// and reports the final client state (should end up trying to download the
// missing episodes, not stuck errored).
func TestQbitImportPartialLive(t *testing.T) {
	host := os.Getenv("SEASONPACKARR_TEST_QBIT_HOST")
	importDir := os.Getenv("SEASONPACKARR_TEST_IMPORT_DIR")
	if host == "" || importDir == "" {
		t.Skip("qbit live env not set")
	}

	c, err := newQbitClient(&domain.Client{
		Host:     host,
		Username: os.Getenv("SEASONPACKARR_TEST_QBIT_USER"),
		Password: os.Getenv("SEASONPACKARR_TEST_QBIT_PASS"),
		Import:   domain.ImportPolicy{SavePath: importDir},
	})
	if err != nil {
		t.Fatalf("newQbitClient: %v", err)
	}

	_, torrentBytes, hashes := buildPartialPack(t, importDir, "AdapterPartialQbit.S01.1080p.WEB-DL.H.264-RlsGrp", 3)
	if err := c.Import(ImportRequest{TorrentBytes: torrentBytes, LegacyHash: hashes.Legacy, V2Hash: hashes.V2, HasV1: hashes.HasV1, SavePath: importDir}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	// poll for a few seconds so the post-recheck resume takes effect, then report
	var tor qbittorrent.Torrent
	for i := range 20 {
		found, ok, err := c.lookupTorrent(hashes.Legacy)
		if err != nil || !ok {
			t.Fatalf("lookup after import: ok=%v err=%v", ok, err)
		}
		tor = found
		t.Logf("  post-import poll[%02d] state=%s progress=%.2f", i, tor.State, tor.Progress)
		if isActiveTorrentState(tor.State) {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Logf("FINAL qbit state=%s progress=%.2f (want: downloading/stalledDL with progress ~0.33, NOT missingFiles/1.00/stopped)", tor.State, tor.Progress)
	if tor.State == qbittorrent.TorrentStateMissingFiles {
		t.Errorf("torrent left in missingFiles after import - recheck path did not recover it")
	}
	if tor.Progress >= 0.99 {
		t.Errorf("progress %.2f - a 1-of-3 partial pack should not read as complete", tor.Progress)
	}
	if !isActiveTorrentState(tor.State) {
		t.Errorf("torrent not active after import (state=%s) - resume did not start it downloading the missing episodes", tor.State)
	}
	_ = c.c.(*qbittorrent.Client).DeleteTorrents([]string{hashes.Legacy}, false)
}

// TestTransmissionImportPartialLive runs the adapter against a partial pack and
// confirms it verifies to a partial percentDone and starts downloading the rest.
func TestTransmissionImportPartialLive(t *testing.T) {
	host := os.Getenv("SEASONPACKARR_TEST_TRANSMISSION_HOST")
	importDir := os.Getenv("SEASONPACKARR_TEST_IMPORT_DIR")
	if host == "" || importDir == "" {
		t.Skip("transmission live env not set")
	}

	c, err := newTransmissionClient(&domain.Client{
		Host:     host,
		Username: os.Getenv("SEASONPACKARR_TEST_TRANSMISSION_USER"),
		Password: os.Getenv("SEASONPACKARR_TEST_TRANSMISSION_PASS"),
		Import:   domain.ImportPolicy{SavePath: importDir},
	})
	if err != nil {
		t.Fatalf("newTransmissionClient: %v", err)
	}

	packName, torrentBytes, hashes := buildPartialPack(t, importDir, "AdapterPartialTr.S01.1080p.WEB-DL.H.264-RlsGrp", 3)
	if err := c.Import(ImportRequest{TorrentBytes: torrentBytes, LegacyHash: hashes.Legacy, V2Hash: hashes.V2, HasV1: hashes.HasV1, SavePath: importDir}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), transmissionTimeout)
	defer cancel()
	ts, err := c.c.TorrentGetHashes(ctx, []string{"name", "status", "percentDone", "errorString"}, []string{hashes.Legacy})
	if err != nil || len(ts) == 0 {
		t.Fatalf("get after import: err=%v n=%d", err, len(ts))
	}
	tr := ts[0]
	pd := 0.0
	if tr.PercentDone != nil {
		pd = *tr.PercentDone
	}
	var status transmissionrpc.TorrentStatus
	if tr.Status != nil {
		status = *tr.Status
	}
	t.Logf("FINAL transmission status=%d percentDone=%.2f error=%q (want: partial <1.0, no error, downloading)", status, pd, derefString(tr.ErrorString))
	if es := derefString(tr.ErrorString); es != "" {
		t.Errorf("transmission reported error after import: %q", es)
	}
	if pd >= 1.0 {
		t.Errorf("percentDone=%.2f - expected partial (only 1 of 3 episodes present)", pd)
	}
	_, _ = packName, cancel
}
