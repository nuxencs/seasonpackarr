// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package seasonpack

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/inventory"
	"github.com/nuxencs/seasonpackarr/internal/logger/loggertest"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient/torrentclienttest"
	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/stretchr/testify/require"
)

// fixture is a processor on an inventory store that gives one fake adapter
// for each client config.
type fixture struct {
	processor *Processor
	store     *inventory.Store
	log       *loggertest.Logger
}

func newFixture(adapter torrentclient.TorrentClient) *fixture {
	log := loggertest.New()
	// The store has its own logger, so log holds only the lines of the processor.
	store := inventory.New(func(context.Context, *domain.Client) (torrentclient.TorrentClient, error) {
		return adapter, nil
	}, loggertest.New())
	return &fixture{processor: New(log, store), store: store, log: log}
}

// client returns the handle of the client name in cfg.
func (f *fixture) client(t *testing.T, name string, cfg domain.Config) *inventory.Client {
	t.Helper()
	client, err := f.store.Client(t.Context(), name, cfg)
	require.NoError(t, err)
	return client
}

// defaultClient returns the handle of the client "default" in a config with
// no smart mode.
func (f *fixture) defaultClient(t *testing.T) *inventory.Client {
	t.Helper()
	return f.client(t, "default", newConfig())
}

// newConfig returns a config with the client "default" and no smart mode.
func newConfig() domain.Config {
	return domain.Config{Clients: map[string]*domain.Client{
		"default": {Type: "qbittorrent", Import: domain.ImportPolicy{Category: "tv-hd"}},
	}}
}

// newSmartConfig returns newConfig with smart mode at threshold.
func newSmartConfig(threshold float32) domain.Config {
	cfg := newConfig()
	cfg.SmartMode = true
	cfg.SmartModeThreshold = threshold
	return cfg
}

// packFixture is a season pack torrent and a fake client that holds the first
// episodes of the pack as episode torrents, with their files on disk.
type packFixture struct {
	name      string
	torrent   []byte
	sourceDir string
	importDir string
	client    *torrentclienttest.Client
}

// newPack makes the pack "<title>.S01.1080p.WEB-DL.H.264-RlsGrp". The client
// holds the episodes from E01 to clientEpisodes, with the hashes "ep1", "ep2", ...
func newPack(t *testing.T, title string, packEpisodes, clientEpisodes int) packFixture {
	t.Helper()
	tempDir := t.TempDir()
	pack := packFixture{
		name:      title + ".S01.1080p.WEB-DL.H.264-RlsGrp",
		sourceDir: filepath.Join(tempDir, "source"),
		importDir: filepath.Join(tempDir, "import"),
	}
	require.NoError(t, os.MkdirAll(pack.sourceDir, 0o755))
	require.NoError(t, os.MkdirAll(pack.importDir, 0o755))

	var err error
	pack.torrent, err = torrents.TorrentFromRls(pack.name, packEpisodes)
	require.NoError(t, err)

	pack.client = &torrentclienttest.Client{
		FilesByHash: make(map[string][]torrentclient.File),
		ImportRoot:  pack.importDir,
	}
	for episode := 1; episode <= clientEpisodes; episode++ {
		hash := fmt.Sprintf("ep%d", episode)
		writeEpisode(t, filepath.Join(pack.sourceDir, pack.episodeFile(episode)))
		pack.client.Torrents = append(pack.client.Torrents, torrentclient.Torrent{
			Name:     pack.episodeRelease(episode),
			Hash:     hash,
			SavePath: pack.sourceDir,
		})
		pack.client.FilesByHash[hash] = []torrentclient.File{{Name: pack.episodeFile(episode), Size: 1}}
	}
	return pack
}

func (p packFixture) episodeRelease(episode int) string {
	title, _, _ := strings.Cut(p.name, ".S01.")
	return fmt.Sprintf("%s.S01E%02d.1080p.WEB-DL.H.264-RlsGrp", title, episode)
}

func (p packFixture) episodeFile(episode int) string {
	return p.episodeRelease(episode) + ".mkv"
}

// source returns the path of the client file of episode.
func (p packFixture) source(episode int) string {
	return filepath.Join(p.sourceDir, p.episodeFile(episode))
}

// target returns the hardlink target of episode below the pack folder.
func (p packFixture) target(episode int) string {
	return filepath.Join(p.importDir, p.name, p.episodeFile(episode))
}

func writeEpisode(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte("0"), 0o644))
}
