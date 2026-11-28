// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// addSourceFolder gives the default client one more source folder, path, and makes it.
func (f *searchFixture) addSourceFolder(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(path, 0o755))
	cfg := f.config.Snapshot()
	client := cfg.Clients["default"].Clone()
	client.SourceFolders = append(client.SourceFolders, path)
	cfg.Clients = map[string]*domain.Client{"default": &client}
	f.config.Store(cfg)
}

// addNewSourceFolder gives the default client a new empty source folder and returns its path.
func (f *searchFixture) addNewSourceFolder(t *testing.T) string {
	t.Helper()
	folder := filepath.Join(t.TempDir(), "source-folder")
	f.addSourceFolder(t, folder)
	return folder
}

// requireImportedFrom fails the test unless the pack folder holds a hardlink of each source file, by its file name.
func (f *searchFixture) requireImportedFrom(t *testing.T, sources ...string) {
	t.Helper()
	for _, source := range sources {
		sourceInfo, err := os.Stat(source)
		require.NoError(t, err)
		targetInfo, err := os.Stat(filepath.Join(f.importDir, f.releaseName, filepath.Base(source)))
		require.NoError(t, err)
		require.True(t, os.SameFile(sourceInfo, targetInfo), "the target of %s must be its hardlink", source)
	}
}

func TestSearch_ImportsSeasonThatIsOnlyInSourceFolder(t *testing.T) {
	f := newSearchFixture(t, fixtureOptions{packEpisodes: 2, threshold: 1})
	cfg := f.config.Snapshot()
	cfg.Search.IndexerIDs = []int{1}
	f.config.Store(cfg)
	folder := f.addNewSourceFolder(t)
	first := filepath.Join(folder, "Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv")
	second := filepath.Join(folder, "Season 01", "Lifecycle.S01E02.1080p.WEB-DL.H.264-RlsGrp.mkv")
	require.NoError(t, os.MkdirAll(filepath.Dir(second), 0o755))
	writeEpisode(t, first)
	writeEpisode(t, second)

	// No scan ran before the search, so the search must scan the folder itself.
	report := f.runExact(t, false)

	require.Empty(t, report.Failures)
	require.Len(t, report.Outcomes, 1)
	require.Equal(t, "imported", report.Outcomes[0].Status, report.Outcomes[0].Reason)
	f.requireImportedFrom(t, first, second)
	assert.Equal(t, []string{"Lifecycle S01"}, f.queries)
	assert.Equal(t, 1, report.Groups)
	assert.Equal(t, 2, report.ScannedFiles)
	assert.Equal(t, 2, report.EpisodeFiles)
	assert.Zero(t, report.CoveredEpisodeFiles)
	assert.Zero(t, report.ScannedTorrents)
	assert.Zero(t, report.EpisodeTorrents)
	assert.Equal(t, new(2), report.Outcomes[0].ReusableEpisodes)
	assert.Equal(t, 1, f.torrentClient.ImportCalls)
}

// The client has the pack of the season, so the files of that season are covered. A torrent episode of another season
// and a file of a third season make one search group each.
func TestSearch_PackInClientCoversSourceFolderFiles(t *testing.T) {
	// Two search groups send two queries to each indexer, so the query gap needs the virtual clock.
	synctest.Test(t, func(t *testing.T) {
		f := newSearchFixture(t, fixtureOptions{packEpisodes: 2})
		f.titles = nil
		f.torrentClient.Torrents = []torrentclient.Torrent{
			{Name: f.releaseName, Hash: "pack"},
			{Name: "Other.Show.S02E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "other"},
		}
		folder := f.addNewSourceFolder(t)
		for _, name := range []string{
			"Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv",
			"Lifecycle.S01E02.1080p.WEB-DL.H.264-RlsGrp.mkv",
			"Third.Show.S03E01.1080p.WEB-DL.H.264-RlsGrp.mkv",
		} {
			writeEpisode(t, filepath.Join(folder, name))
		}

		report := f.runExact(t, true)

		require.Empty(t, report.Failures)
		assert.Equal(t, 2, report.ScannedTorrents)
		assert.Equal(t, 1, report.EpisodeTorrents)
		assert.Zero(t, report.CoveredEpisodeTorrents)
		assert.Equal(t, 3, report.ScannedFiles)
		assert.Equal(t, 3, report.EpisodeFiles)
		assert.Equal(t, 2, report.CoveredEpisodeFiles)
		assert.Equal(t, 2, report.Groups)
		assert.ElementsMatch(t, []string{"Other Show S02", "Other Show S02", "Third Show S03", "Third Show S03"}, f.queries)
	})
}

// A file in two source folders of the client, one below the other, counts once.
func TestSearch_CountsAFileOfNestedSourceFoldersOnce(t *testing.T) {
	f := newSearchFixture(t, fixtureOptions{packEpisodes: 1})
	f.titles = nil
	folder := f.addNewSourceFolder(t)
	season := filepath.Join(folder, "Lifecycle", "Season 01")
	f.addSourceFolder(t, season)
	writeEpisode(t, filepath.Join(season, "Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"))

	report := f.runExact(t, true)

	require.Empty(t, report.Failures)
	assert.Equal(t, 1, report.ScannedFiles)
	assert.Equal(t, 1, report.EpisodeFiles)
	assert.Equal(t, 1, report.Groups)
}

// With no source folder, the file fields are 0 and the torrent fields are as before.
func TestSearch_ReportWithNoSourceFolderHasZeroFileFields(t *testing.T) {
	f := newSearchFixture(t, fixtureOptions{packEpisodes: 2, clientEpisodes: 2})

	response := f.postJSON(t, "/api/search", SearchRequest{DryRun: true})

	require.Equal(t, 200, response.Code, response.Body.String())
	var report map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &report))
	for field, want := range map[string]float64{
		"scannedTorrents": 2, "episodeTorrents": 2, "coveredEpisodeTorrents": 0,
		"scannedFiles": 0, "episodeFiles": 0, "coveredEpisodeFiles": 0,
		"groups": 1,
	} {
		if assert.Contains(t, report, field) {
			assert.Equal(t, want, report[field], field)
		}
	}
}

// A file that arrived after the last scan counts, because each search run scans the source folders first.
func TestSearch_ScansSourceFoldersAtStart(t *testing.T) {
	f := newSearchFixture(t, fixtureOptions{packEpisodes: 1})
	f.titles = nil
	folder := f.addNewSourceFolder(t)
	f.inventory.ReadAtStartup(t.Context(), f.config)
	writeEpisode(t, filepath.Join(folder, "Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"))

	report := f.runExact(t, true)

	require.Empty(t, report.Failures)
	assert.Equal(t, 1, report.Groups)
	assert.Equal(t, 1, report.EpisodeFiles)
	assert.Equal(t, []string{"Lifecycle S01", "Lifecycle S01"}, f.queries)
}

// The cap and the wait time count a search group from a source folder as any other search group.
func TestSearch_LimitAndWaitTimeCountSourceFolderGroups(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSearchFixture(t, fixtureOptions{packEpisodes: 1, clientEpisodes: 1, threshold: 1})
		f.titles = nil
		// One torrent group, "Lifecycle S01", and one file group, "Aaa001 S01", which sorts first. Indexer 1 alone
		// gives each group one query.
		cfg := f.config.Snapshot()
		cfg.Search.IndexerIDs = []int{1}
		f.config.Store(cfg)
		folder := f.addNewSourceFolder(t)
		writeEpisode(t, filepath.Join(folder, "Aaa001.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"))

		first := f.searchFor(t, SearchRequest{Limit: new(1)})
		require.Empty(t, first.Failures)
		assert.Equal(t, 2, first.Groups)
		assert.Equal(t, 1, first.GroupsDeferred)
		assert.Equal(t, []string{"Aaa001 S01"}, f.queries)

		time.Sleep(time.Hour)
		f.queries = nil
		second := f.searchFor(t, SearchRequest{Limit: new(1)})
		require.Empty(t, second.Failures)
		assert.Equal(t, 1, second.GroupsNotDue)
		assert.Zero(t, second.GroupsDeferred)
		assert.Equal(t, []string{"Lifecycle S01"}, f.queries)
	})
}

func TestRSS_ImportsSeasonThatIsOnlyInSourceFolder(t *testing.T) {
	f := newSearchFixture(t, fixtureOptions{packEpisodes: 2, threshold: 1})
	folder := f.addNewSourceFolder(t)
	files := []string{
		filepath.Join(folder, "Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"),
		filepath.Join(folder, "Lifecycle.S01E02.1080p.WEB-DL.H.264-RlsGrp.mkv"),
	}
	for _, file := range files {
		writeEpisode(t, file)
	}
	f.inventory.ReadAtStartup(t.Context(), f.config)

	report := f.runRSS(t)

	require.Empty(t, report.Failures)
	require.NotEmpty(t, report.Outcomes)
	require.Equal(t, "imported", report.Outcomes[0].Status, report.Outcomes[0].Reason)
	f.requireImportedFrom(t, files...)
	assert.Equal(t, 1, report.Groups)
	assert.Equal(t, 2, report.EpisodeFiles)
	assert.Equal(t, 1, f.torrentClient.ImportCalls)
}

// An RSS poll starts no scan, so a file that arrived after the last scan makes no search group yet.
func TestRSS_StartsNoSourceFolderScan(t *testing.T) {
	f := newSearchFixture(t, fixtureOptions{packEpisodes: 1, threshold: 1})
	folder := f.addNewSourceFolder(t)
	f.inventory.ReadAtStartup(t.Context(), f.config)
	writeEpisode(t, filepath.Join(folder, "Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"))

	report := f.runRSS(t)

	require.Empty(t, report.Failures)
	assert.Zero(t, report.Groups)
	assert.Zero(t, report.ScannedFiles)
	assert.Zero(t, report.Requests)
	assert.Zero(t, f.torrentClient.ImportCalls)
}
