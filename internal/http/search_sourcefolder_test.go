// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/stretchr/testify/require"
)

// Search makes its search groups from torrent entries only, so a source folder changes no report value. The folder
// holds a season that no torrent has, which would add a group.
func TestSearch_ReportIgnoresSourceFolderFiles(t *testing.T) {
	f := newSearchFixture(t, fixtureOptions{packEpisodes: 2, clientEpisodes: 2})
	folder := filepath.Join(t.TempDir(), "source-folder")
	require.NoError(t, os.MkdirAll(folder, 0o755))
	writeEpisode(t, filepath.Join(folder, "Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"))
	writeEpisode(t, filepath.Join(folder, "Unrelated.S02E01.1080p.WEB-DL.H.264-RlsGrp.mkv"))
	cfg := f.config.Snapshot()
	client := cfg.Clients["default"].Clone()
	client.SourceFolders = []string{folder}
	cfg.Clients = map[string]*domain.Client{"default": &client}
	f.config.Store(cfg)
	f.inventory.ReadAtStartup(t.Context(), f.config)

	report := f.runExact(t, true)

	require.Empty(t, report.Failures)
	require.Equal(t, 1, report.Groups)
	require.Equal(t, 2, report.ScannedTorrents)
	require.Equal(t, 2, report.EpisodeTorrents)
	require.Zero(t, report.CoveredEpisodeTorrents)
	require.Equal(t, []string{"Lifecycle S01", "Lifecycle S01"}, f.queries)
}
