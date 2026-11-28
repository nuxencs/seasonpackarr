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
		SavePath:    pack.sourceDir,
		ImportRoot:  pack.importDir,
	}
	for episode := 1; episode <= clientEpisodes; episode++ {
		hash := fmt.Sprintf("ep%d", episode)
		writeEpisode(t, filepath.Join(pack.sourceDir, pack.episodeFile(episode)))
		pack.client.Torrents = append(pack.client.Torrents, torrentclient.Torrent{
			Name: pack.episodeRelease(episode),
			Hash: hash,
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

// moveEpisode moves the client file of episode to another folder, as a
// client relocation does, and makes the fake client report the new save path.
func (p packFixture) moveEpisode(t *testing.T, episode int) {
	t.Helper()
	movedDir := filepath.Join(filepath.Dir(p.sourceDir), "source-moved")
	require.NoError(t, os.MkdirAll(movedDir, 0o755))
	require.NoError(t, os.Rename(p.source(episode), filepath.Join(movedDir, p.episodeFile(episode))))
	if p.client.SavePathByHash == nil {
		p.client.SavePathByHash = make(map[string]string)
	}
	p.client.SavePathByHash[fmt.Sprintf("ep%d", episode)] = movedDir
}

// target returns the hardlink target of episode below the pack folder.
func (p packFixture) target(episode int) string {
	return filepath.Join(p.importDir, p.name, p.episodeFile(episode))
}

func writeEpisode(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte("0"), 0o644))
}

// fakeConfig is a config provider with one snapshot.
type fakeConfig domain.Config

func (f fakeConfig) Snapshot() domain.Config { return domain.Config(f) }

// withSourceFolders returns cfg with folders as the source folders of the client "default".
func withSourceFolders(cfg domain.Config, folders ...string) domain.Config {
	client := cfg.Clients["default"].Clone()
	client.SourceFolders = folders
	cfg.Clients = map[string]*domain.Client{"default": &client}
	return cfg
}

// scanSourceFolders scans the source folders of cfg through the startup read, as the server does.
func (f *fixture) scanSourceFolders(t *testing.T, cfg domain.Config) {
	t.Helper()
	f.store.ReadAtStartup(t.Context(), fakeConfig(cfg))
}

// folder returns the path of a source folder of the pack. The pack fixture does not make it.
func (p packFixture) folder() string {
	return filepath.Join(filepath.Dir(p.sourceDir), "source-folder")
}

// writeFolderFile writes the file of episode below the source folder, in a season folder as Sonarr does, and returns
// its path.
func (p packFixture) writeFolderFile(t *testing.T, episode int) string {
	t.Helper()
	return p.writeFolderFileNamed(t, p.episodeFile(episode))
}

// writeFolderFileNamed writes a one-byte file with name below the season folder of the source folder.
func (p packFixture) writeFolderFileNamed(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(p.folder(), "Season 01", name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	writeEpisode(t, path)
	return path
}

// requireSameFile fails the test unless path and other are hardlinks of one file.
func requireSameFile(t *testing.T, path, other string) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	otherInfo, err := os.Stat(other)
	require.NoError(t, err)
	require.True(t, os.SameFile(info, otherInfo), "%s must be a hardlink of %s", other, path)
}
