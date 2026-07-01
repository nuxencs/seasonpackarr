// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
)

// mockTorrentClient is a configurable in-memory torrentclient.TorrentClient for
// exercising the processor without a live torrent client.
type mockTorrentClient struct {
	torrents    []torrentclient.Torrent
	torrentsErr error
	files       []torrentclient.File
	filesErr    error
	gotHash     string
}

func (m *mockTorrentClient) GetTorrents() ([]torrentclient.Torrent, error) {
	return m.torrents, m.torrentsErr
}

func (m *mockTorrentClient) GetFiles(hash string) ([]torrentclient.File, error) {
	m.gotHash = hash
	return m.files, m.filesErr
}

func newTestProcessor(client torrentclient.TorrentClient) *processor {
	return &processor{req: &request{Client: client}}
}

// TestTorrentClientPathContract locks in the load-bearing contract documented on
// torrentclient.TorrentClient: the hardlink source the processor uses is
// filepath.Join(Torrent.SavePath, <file name from GetFiles>). File names keep
// their torrent-root-folder prefix and SavePath is the absolute on-disk download
// dir, so the join is the real file path. A future TorrentClient implementation
// that returns a bare basename or a non-absolute SavePath would break hardlinking
// silently — this test fails first.
func TestTorrentClientPathContract(t *testing.T) {
	const savePath = "/data/torrents/tv"
	mock := &mockTorrentClient{
		torrents: []torrentclient.Torrent{
			{Hash: "abc123", Name: "Some.Show.S01.1080p", SavePath: savePath},
		},
		files: []torrentclient.File{
			{Name: "Some.Show.S01.1080p/Some.Show.S01E01.1080p.mkv", Size: 1_000_000},
		},
	}
	p := newTestProcessor(mock)

	ts, err := p.req.Client.GetTorrents()
	if err != nil {
		t.Fatalf("GetTorrents: %v", err)
	}
	if len(ts) != 1 {
		t.Fatalf("len(torrents) = %d, want 1", len(ts))
	}

	fileName, size, err := p.getFiles(ts[0].Hash)
	if err != nil {
		t.Fatalf("getFiles: %v", err)
	}
	if mock.gotHash != "abc123" {
		t.Errorf("GetFiles called with hash %q, want abc123", mock.gotHash)
	}
	if size != 1_000_000 {
		t.Errorf("size = %d, want 1000000", size)
	}

	gotSource := filepath.Join(ts[0].SavePath, fileName)
	const wantSource = "/data/torrents/tv/Some.Show.S01.1080p/Some.Show.S01E01.1080p.mkv"
	if gotSource != wantSource {
		t.Errorf("hardlink source = %q, want %q", gotSource, wantSource)
	}
}

func TestGetFilesSelectsFirstValidEpisode(t *testing.T) {
	mock := &mockTorrentClient{
		files: []torrentclient.File{
			{Name: "Some.Show.S01/poster.jpg", Size: 50},                  // not a video
			{Name: "Some.Show.S01/Some.Show.S01E01.mkv", Size: 1_000_000}, // first valid
			{Name: "Some.Show.S01/Some.Show.S01E02.mkv", Size: 1_100_000},
		},
	}
	p := newTestProcessor(mock)

	fileName, size, err := p.getFiles("h")
	if err != nil {
		t.Fatalf("getFiles: %v", err)
	}
	if fileName != "Some.Show.S01/Some.Show.S01E01.mkv" {
		t.Errorf("fileName = %q, want Some.Show.S01/Some.Show.S01E01.mkv", fileName)
	}
	if size != 1_000_000 {
		t.Errorf("size = %d, want 1000000", size)
	}
}

func TestGetFilesErrorsWhenNoValidEpisode(t *testing.T) {
	mock := &mockTorrentClient{
		files: []torrentclient.File{
			{Name: "Some.Show.S01/poster.jpg", Size: 50},
			{Name: "Some.Show.S01/readme.txt", Size: 10},
		},
	}
	p := newTestProcessor(mock)

	if _, _, err := p.getFiles("h"); err == nil {
		t.Fatal("expected error when no valid episode file is present, got nil")
	}
}

func TestGetFilesPropagatesClientError(t *testing.T) {
	errBoom := errors.New("boom")
	p := newTestProcessor(&mockTorrentClient{filesErr: errBoom})

	if _, _, err := p.getFiles("h"); !errors.Is(err, errBoom) {
		t.Fatalf("getFiles error = %v, want it to wrap errBoom", err)
	}
}
