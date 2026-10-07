// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build integration

package torrentclient

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	return buildPartialPackKeeping(t, importDir, packName, episodes, 1)
}

// buildPartialPackKeeping is buildPartialPack, but it keeps only the 1-based
// episode keep on disk.
func buildPartialPackKeeping(t *testing.T, importDir, packName string, episodes, keep int) (string, []byte, torrents.Hashes) {
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
	// delete all but the kept episode to simulate a partial pack
	for i, ep := range names {
		if i+1 == keep {
			continue
		}
		if err := os.Remove(filepath.Join(packDir, ep)); err != nil {
			t.Fatalf("remove: %v", err)
		}
	}
	return packName, torrentBytes, hashes
}

// TestQbitPartialRawBehavior_ReportsPausedMissingFileState answers the load-bearing question: does real
// qBittorrent report missingFiles for a PAUSED, skip-checked torrent whose files
// are partially missing, or only once resumed? This is what the adapter's
// "if state == missingFiles { recheck }" branch depends on.
func TestQbitPartialRawBehavior_ReportsPausedMissingFileState(t *testing.T) {
	host := os.Getenv("SEASONPACKARR_TEST_QBIT_HOST")
	importDir := os.Getenv("SEASONPACKARR_TEST_IMPORT_DIR")
	if host == "" || importDir == "" {
		t.Skip("qBittorrent integration environment is not set")
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

// TestQbitImport_ResumesPartialPack runs the actual adapter Import against a partial pack
// and reports the final client state (should end up trying to download the
// missing episodes, not stuck errored).
func TestQbitImport_ResumesPartialPack(t *testing.T) {
	host := os.Getenv("SEASONPACKARR_TEST_QBIT_HOST")
	importDir := os.Getenv("SEASONPACKARR_TEST_IMPORT_DIR")
	if host == "" || importDir == "" {
		t.Skip("qBittorrent integration environment is not set")
	}

	c, err := newQbitClient(t.Context(), &domain.Client{
		Host:     host,
		Username: os.Getenv("SEASONPACKARR_TEST_QBIT_USER"),
		Password: os.Getenv("SEASONPACKARR_TEST_QBIT_PASS"),
		Import:   domain.ImportPolicy{SavePath: importDir},
	})
	if err != nil {
		t.Fatalf("newQbitClient: %v", err)
	}

	_, torrentBytes, hashes := buildPartialPack(t, importDir, "AdapterPartialQbit.S01.1080p.WEB-DL.H.264-RlsGrp", 3)
	if _, err := c.Import(t.Context(), ImportRequest{TorrentBytes: torrentBytes, LegacyHash: hashes.Legacy, V2Hash: hashes.V2, HasV1: hashes.HasV1, SavePath: importDir}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	// poll for a few seconds so qBittorrent's add-time check finishes, then report
	var tor qbittorrent.Torrent
	for i := range 20 {
		found, ok, err := c.lookupTorrent(t.Context(), hashes.Legacy)
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
		t.Errorf("torrent left in missingFiles after import - a partial pack must use a normal check")
	}
	if tor.Progress >= 0.99 {
		t.Errorf("progress %.2f - a 1-of-3 partial pack should not read as complete", tor.Progress)
	}
	if !isActiveTorrentState(tor.State) {
		t.Errorf("torrent not active after import (state=%s) - qBittorrent did not start it after the check", tor.State)
	}
	_ = c.c.(*qbittorrent.Client).DeleteTorrents([]string{hashes.Legacy}, false)
}

// TestQbitImport_RecoversMisclassifiedCompletePack drives the complete-pack
// fallback against a real daemon: a skip-check add of a partial pack reports
// missingFiles, and recheck, stop, start must leave qBittorrent to check and
// start it. This depends on stop clearing the FilesChecked stop condition.
func TestQbitImport_RecoversMisclassifiedCompletePack(t *testing.T) {
	host := os.Getenv("SEASONPACKARR_TEST_QBIT_HOST")
	importDir := os.Getenv("SEASONPACKARR_TEST_IMPORT_DIR")
	if host == "" || importDir == "" {
		t.Skip("qBittorrent integration environment is not set")
	}

	c, err := newQbitClient(t.Context(), &domain.Client{
		Host:     host,
		Username: os.Getenv("SEASONPACKARR_TEST_QBIT_USER"),
		Password: os.Getenv("SEASONPACKARR_TEST_QBIT_PASS"),
		Import:   domain.ImportPolicy{SavePath: importDir},
	})
	if err != nil {
		t.Fatalf("newQbitClient: %v", err)
	}

	_, torrentBytes, hashes := buildPartialPack(t, importDir, "AdapterFallbackQbit.S01.1080p.WEB-DL.H.264-RlsGrp", 3)
	report, err := c.Import(t.Context(), ImportRequest{TorrentBytes: torrentBytes, LegacyHash: hashes.Legacy, V2Hash: hashes.V2, HasV1: hashes.HasV1, SavePath: importDir, DataComplete: true})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if !slices.ContainsFunc(report.Stages, func(s ImportStageReport) bool { return s.Stage == ImportStageRecheck }) {
		t.Fatalf("stages %v: want the missingFiles fallback to run", report.Stages)
	}

	var tor qbittorrent.Torrent
	for range 30 {
		found, ok, err := c.lookupTorrent(t.Context(), hashes.Legacy)
		if err != nil || !ok {
			t.Fatalf("lookup after import: ok=%v err=%v", ok, err)
		}
		tor = found
		if isActiveTorrentState(tor.State) {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Logf("FINAL qbit state=%s progress=%.2f", tor.State, tor.Progress)
	if !isActiveTorrentState(tor.State) {
		t.Errorf("torrent not active after the fallback (state=%s) - the recheck stop condition was not cleared", tor.State)
	}
	if tor.Progress <= 0 || tor.Progress >= 0.99 {
		t.Errorf("progress %.2f - a 1-of-3 partial pack must be checked to a partial value", tor.Progress)
	}
	_ = c.c.(*qbittorrent.Client).DeleteTorrents([]string{hashes.Legacy}, false)
}

// TestTransmissionImport_ResumesPartialPack runs the adapter against a partial pack and
// confirms it verifies to a partial percentDone and starts downloading the rest.
func TestTransmissionImport_ResumesPartialPack(t *testing.T) {
	host := os.Getenv("SEASONPACKARR_TEST_TRANSMISSION_HOST")
	importDir := os.Getenv("SEASONPACKARR_TEST_IMPORT_DIR")
	if host == "" || importDir == "" {
		t.Skip("Transmission integration environment is not set")
	}

	c, err := newTransmissionClient(t.Context(), &domain.Client{
		Host:     host,
		Username: os.Getenv("SEASONPACKARR_TEST_TRANSMISSION_USER"),
		Password: os.Getenv("SEASONPACKARR_TEST_TRANSMISSION_PASS"),
		Import:   domain.ImportPolicy{SavePath: importDir},
	})
	if err != nil {
		t.Fatalf("newTransmissionClient: %v", err)
	}

	packName, torrentBytes, hashes := buildPartialPack(t, importDir, "AdapterPartialTr.S01.1080p.WEB-DL.H.264-RlsGrp", 3)
	if _, err := c.Import(t.Context(), ImportRequest{TorrentBytes: torrentBytes, LegacyHash: hashes.Legacy, V2Hash: hashes.V2, HasV1: hashes.HasV1, SavePath: importDir}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	tr := waitTransmissionChecked(t, c, hashes.Legacy)
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
	if pd >= 1.0 || pd <= 0 {
		t.Errorf("percentDone=%.2f - expected partial (only 1 of 3 episodes present)", pd)
	}
	if status == transmissionrpc.TorrentStatusStopped {
		t.Errorf("import left the torrent stopped - Transmission must start it after the check")
	}
	_ = packName
}

// TestTransmissionImport_IncompleteDirPartialPack covers Transmission with an
// incomplete folder. When the first torrent file is missing, Transmission keeps
// the torrent's current folder in the incomplete folder, but it must still find
// the hardlinked episodes in the download folder, check them, and start.
func TestTransmissionImport_IncompleteDirPartialPack(t *testing.T) {
	host := os.Getenv("SEASONPACKARR_TEST_TRANSMISSION_HOST")
	importDir := os.Getenv("SEASONPACKARR_TEST_IMPORT_DIR")
	if host == "" || importDir == "" {
		t.Skip("Transmission integration environment is not set")
	}

	c, err := newTransmissionClient(t.Context(), &domain.Client{
		Host:     host,
		Username: os.Getenv("SEASONPACKARR_TEST_TRANSMISSION_USER"),
		Password: os.Getenv("SEASONPACKARR_TEST_TRANSMISSION_PASS"),
		Import:   domain.ImportPolicy{SavePath: importDir},
	})
	if err != nil {
		t.Fatalf("newTransmissionClient: %v", err)
	}
	raw, ok := c.c.(*transmissionrpc.Client)
	if !ok {
		t.Fatal("Transmission client does not expose session settings")
	}
	original, err := raw.SessionArgumentsGetAll(t.Context())
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	t.Cleanup(func() {
		if err := raw.SessionArgumentsSet(context.WithoutCancel(t.Context()), transmissionrpc.SessionArguments{
			IncompleteDirEnabled: original.IncompleteDirEnabled,
			IncompleteDir:        original.IncompleteDir,
		}); err != nil {
			t.Errorf("restore session: %v", err)
		}
	})
	incompleteDir := filepath.Join(importDir, "incomplete")
	if err := os.MkdirAll(incompleteDir, 0o755); err != nil {
		t.Fatalf("mkdir incomplete dir: %v", err)
	}
	if err := raw.SessionArgumentsSet(t.Context(), transmissionrpc.SessionArguments{
		IncompleteDirEnabled: new(true),
		IncompleteDir:        new(incompleteDir),
	}); err != nil {
		t.Fatalf("enable incomplete dir: %v", err)
	}

	_, torrentBytes, hashes := buildPartialPackKeeping(t, importDir, "AdapterIncompleteTr.S01.1080p.WEB-DL.H.264-RlsGrp", 3, 3)
	if _, err := c.Import(t.Context(), ImportRequest{TorrentBytes: torrentBytes, LegacyHash: hashes.Legacy, V2Hash: hashes.V2, HasV1: hashes.HasV1, SavePath: importDir}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	tr := waitTransmissionChecked(t, c, hashes.Legacy)
	pd := 0.0
	if tr.PercentDone != nil {
		pd = *tr.PercentDone
	}
	t.Logf("FINAL transmission status=%d percentDone=%.2f error=%q", *tr.Status, pd, derefString(tr.ErrorString))
	if es := derefString(tr.ErrorString); es != "" {
		t.Errorf("transmission reported error after import: %q", es)
	}
	if pd <= 0 || pd >= 1.0 {
		t.Errorf("percentDone=%.2f - the hardlinked last episode must be found outside the incomplete folder", pd)
	}
	if *tr.Status == transmissionrpc.TorrentStatusStopped {
		t.Errorf("import left the torrent stopped - Transmission must start it after the check")
	}
}
