// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package inventory

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger/loggertest"
	"github.com/nuxencs/seasonpackarr/internal/release"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"

	"github.com/autobrr/rls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scheduledScans is a store that runs the startup read and the scan schedule, as the server does. Each test runs in a
// synctest bubble, so the timer uses the virtual clock.
type scheduledScans struct {
	store    *Store
	log      *loggertest.Logger
	provider *fakeProvider
	stopped  chan struct{}
}

// newScheduledScans returns a store whose client reads one torrent. Its schedule does not run yet.
func newScheduledScans(cfg domain.Config) *scheduledScans {
	adapter := &fakeAdapter{torrents: []torrentclient.Torrent{{Name: "Series.S02E01.1080p.WEB-DL-GRP"}}}
	s := &scheduledScans{log: loggertest.New(), provider: &fakeProvider{cfg: cfg}, stopped: make(chan struct{})}
	s.store = New((&fakeConnector{adapter: adapter}).connect, s.log)
	return s
}

// startScheduledScans runs the startup read and then returns, while the scan schedule runs until the test ends.
func startScheduledScans(t *testing.T, cfg domain.Config) *scheduledScans {
	t.Helper()
	s := newScheduledScans(cfg)
	s.start(t)
	return s
}

func (s *scheduledScans) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		defer close(s.stopped)
		s.store.ScheduleSourceFolderScans(ctx, s.provider)
	}()
	// The schedule must return before the bubble ends.
	t.Cleanup(func() {
		cancel()
		<-s.stopped
	})
	s.store.ReadAtStartup(ctx, s.provider)
	synctest.Wait()
}

// reload stores cfg and runs the clean-up, as the reload parties of the start command do.
func (s *scheduledScans) reload(t *testing.T, cfg domain.Config) {
	t.Helper()
	s.provider.store(cfg)
	_, err := s.store.CleanUp(cfg)
	require.NoError(t, err)
	synctest.Wait()
}

// scanCounts returns the episode file count of each finished scan of folder, in order.
func (s *scheduledScans) scanCounts(t *testing.T, folder string) []int {
	t.Helper()
	var counts []int
	for _, event := range s.log.Events(t) {
		if event["message"] == sourceFolderScanned && event["folder"] == folder {
			counts = append(counts, int(event["episode_files"].(float64)))
		}
	}
	return counts
}

// folderConfig returns a config with one client that names folders, and the scan interval.
func folderConfig(interval string, folders ...string) domain.Config {
	cfg := configWith(map[string]*domain.Client{"default": {Host: "qbit", SourceFolders: folders}})
	cfg.SourceFolderScanInterval = interval
	return cfg
}

func TestScheduleSourceFolderScans_ScansAgainAfterEachInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root := t.TempDir()
		writeSourceFile(t, root, keptEpisode)
		scans := startScheduledScans(t, folderConfig("15m", root))
		require.Equal(t, []int{1}, scans.scanCounts(t, root), "the startup read scans the folder")

		writeSourceFile(t, root, "Series.S01E02.1080p.WEB-DL-GRP.mkv")
		time.Sleep(15*time.Minute - time.Nanosecond)
		synctest.Wait()
		assert.Equal(t, []int{1}, scans.scanCounts(t, root), "no scan before the interval ends")

		time.Sleep(time.Nanosecond)
		synctest.Wait()
		assert.Equal(t, []int{1, 2}, scans.scanCounts(t, root), "a file that arrived after the startup scan counts after one interval")
		assert.Equal(t, 2, scans.store.sourceFolderFiles([]string{root}))

		time.Sleep(15 * time.Minute)
		synctest.Wait()
		assert.Equal(t, []int{1, 2, 2}, scans.scanCounts(t, root), "the scans repeat")
	})
}

func TestScheduleSourceFolderScans_KeepsTheLastResultOfAFolderThatCannotBeRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mount := t.TempDir()
		root := filepath.Join(mount, "tv")
		writeSourceFile(t, root, keptEpisode)
		writeSourceFile(t, root, "Series.S01E02.1080p.WEB-DL-GRP.mkv")
		scans := startScheduledScans(t, folderConfig("15m", root))
		require.Equal(t, []int{2}, scans.scanCounts(t, root))

		// A mount that is gone for a short time looks like a folder that does not exist.
		hidden := filepath.Join(mount, "gone")
		require.NoError(t, os.Rename(root, hidden))
		time.Sleep(15 * time.Minute)
		synctest.Wait()

		events := scans.log.Events(t)
		warning := events.RequireField(t, "could not read source folder", "folder", root)
		assert.Equal(t, "warn", warning["level"])
		assert.Equal(t, "default", warning["clientname"])
		assert.Equal(t, []int{2}, scans.scanCounts(t, root), "a failed scan has no scan line")
		assert.Equal(t, 2, scans.store.sourceFolderFiles([]string{root}), "a failed scan keeps the last good result")

		require.NoError(t, os.Rename(hidden, root))
		require.NoError(t, os.Remove(filepath.Join(root, keptEpisode)))
		time.Sleep(15 * time.Minute)
		synctest.Wait()

		assert.Equal(t, []int{2, 1}, scans.scanCounts(t, root))
		assert.Equal(t, 1, scans.store.sourceFolderFiles([]string{root}), "the next good scan replaces the result")
	})
}

func TestScheduleSourceFolderScans_CountsAFolderThatWasMissingAtStartup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "tv")
		scans := startScheduledScans(t, folderConfig("15m", root))
		scans.log.Events(t).RequireField(t, "could not read source folder", "folder", root)

		writeSourceFile(t, root, keptEpisode)
		time.Sleep(15 * time.Minute)
		synctest.Wait()

		assert.Equal(t, []int{1}, scans.scanCounts(t, root), "the first scan that can read the folder counts it, with no restart")
	})
}

func TestScheduleSourceFolderScans_ParsesNoKnownNameAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root := t.TempDir()
		writeSourceFile(t, root, keptEpisode)
		writeSourceFile(t, root, "Season 01/Series.S01E02.1080p.WEB-DL-GRP.mkv")
		writeSourceFile(t, root, "Series (2023) - S01E04 - Episode Title [WEBDL-1080p].mkv")
		// The parser rejects these names. The scan must not parse them again either.
		writeSourceFile(t, root, "a3f9c1e07b2d4e8f9a0b.mkv")
		writeSourceFile(t, root, "Series.S01E05.1080p.WEB-DL-GRP-sample.mkv")
		// The extension check rejects a name before the parser.
		writeSourceFile(t, root, "Series.S01E01.1080p.WEB-DL-GRP.nfo")
		scans := newScheduledScans(folderConfig("15m", root))
		var mu sync.Mutex
		var parsed []string
		scans.store.parseEpisodeName = func(path string) (*rls.Release, bool) {
			mu.Lock()
			defer mu.Unlock()
			parsed = append(parsed, filepath.Base(path))
			return release.ParseEpisodeName(path)
		}
		scans.start(t)

		writeSourceFile(t, root, "Series.S01E03.1080p.WEB-DL-GRP.mkv")
		time.Sleep(15 * time.Minute)
		synctest.Wait()

		require.Equal(t, []int{3, 4}, scans.scanCounts(t, root))
		mu.Lock()
		defer mu.Unlock()
		assert.ElementsMatch(t, []string{
			keptEpisode, "Series.S01E02.1080p.WEB-DL-GRP.mkv", "a3f9c1e07b2d4e8f9a0b.mkv",
			"Series (2023) - S01E04 - Episode Title [WEBDL-1080p].mkv", "Series.S01E05.1080p.WEB-DL-GRP-sample.mkv",
			"Series.S01E03.1080p.WEB-DL-GRP.mkv",
		}, parsed, "the second scan parses only the new name")
	})
}

func TestScheduleSourceFolderScans_ScansANewFolderOfAReloadAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		first, second := t.TempDir(), t.TempDir()
		writeSourceFile(t, first, keptEpisode)
		writeSourceFile(t, second, keptEpisode)
		writeSourceFile(t, second, "Series.S01E02.1080p.WEB-DL-GRP.mkv")
		scans := startScheduledScans(t, folderConfig("15m", first))
		time.Sleep(5 * time.Minute)

		scans.reload(t, folderConfig("15m", first, second))

		assert.Equal(t, []int{2}, scans.scanCounts(t, second), "the new folder gets a scan at once")
		assert.Equal(t, []int{1}, scans.scanCounts(t, first), "a known folder waits for the interval")
		time.Sleep(10 * time.Minute)
		synctest.Wait()
		assert.Equal(t, []int{1, 1}, scans.scanCounts(t, first), "the reload does not move the interval")
		assert.Equal(t, []int{2, 2}, scans.scanCounts(t, second))
	})
}

func TestScheduleSourceFolderScans_DropsTheResultOfAFolderThatAReloadRemoved(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		kept, removed := t.TempDir(), t.TempDir()
		writeSourceFile(t, kept, keptEpisode)
		writeSourceFile(t, removed, keptEpisode)
		cfg := configWith(map[string]*domain.Client{
			"hd":  {Host: "qbit", Import: domain.ImportPolicy{Category: "tv-hd"}, SourceFolders: []string{kept, removed}},
			"uhd": {Host: "qbit", Import: domain.ImportPolicy{Category: "tv-uhd"}, SourceFolders: []string{removed}},
		})
		scans := startScheduledScans(t, cfg)

		// One client still names the folder, so its result stays.
		cfg = configWith(map[string]*domain.Client{
			"hd":  {Host: "qbit", Import: domain.ImportPolicy{Category: "tv-hd"}, SourceFolders: []string{kept}},
			"uhd": {Host: "qbit", Import: domain.ImportPolicy{Category: "tv-uhd"}, SourceFolders: []string{removed}},
		})
		scans.reload(t, cfg)
		assert.Equal(t, 1, scans.store.sourceFolderFiles([]string{removed}))

		scans.reload(t, folderConfig("15m", kept))
		assert.Zero(t, scans.store.sourceFolderFiles([]string{removed}), "no client names the folder")
		assert.Equal(t, 1, scans.store.sourceFolderFiles([]string{kept}))

		time.Sleep(15 * time.Minute)
		synctest.Wait()
		assert.Equal(t, []int{1}, scans.scanCounts(t, removed), "the schedule does not scan a removed folder")
		assert.Equal(t, []int{1, 1}, scans.scanCounts(t, kept))

		scans.reload(t, folderConfig("15m", kept, removed))
		assert.Equal(t, []int{1, 1}, scans.scanCounts(t, removed), "a folder that comes back is new again")
	})
}

func TestScheduleSourceFolderScans_AppliesTheIntervalOfAReload(t *testing.T) {
	tests := map[string]struct {
		reloadAfter time.Duration
		interval    string
		wantScanAt  time.Duration
	}{
		"longer":                          {reloadAfter: 5 * time.Minute, interval: "1h", wantScanAt: time.Hour},
		"shorter":                         {reloadAfter: 5 * time.Minute, interval: "10m", wantScanAt: 10 * time.Minute},
		"shorter, with the due time past": {reloadAfter: 5 * time.Minute, interval: "1m", wantScanAt: 5 * time.Minute},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				root := t.TempDir()
				writeSourceFile(t, root, keptEpisode)
				started := time.Now()
				scans := startScheduledScans(t, folderConfig("15m", root))
				time.Sleep(tt.reloadAfter)

				scans.reload(t, folderConfig(tt.interval, root))
				// The bound fails a schedule that never scans, instead of a test that never ends.
				for len(scans.scanCounts(t, root)) < 2 && time.Since(started) < 2*time.Hour {
					time.Sleep(time.Second)
					synctest.Wait()
				}

				assert.Equal(t, tt.wantScanAt, time.Since(started).Truncate(time.Second),
					"the next scan starts one new interval after the end of the last scan")
			})
		})
	}
}

func TestReadAtStartup_StoresNoResultForAFolderThatACleanUpDroppedDuringTheScan(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root := t.TempDir()
		writeSourceFile(t, root, keptEpisode)
		scans := newScheduledScans(folderConfig("15m", root))
		// A reload removes the folder while the startup scan walks it.
		scans.store.parseEpisodeName = func(path string) (*rls.Release, bool) {
			removed := folderConfig("15m")
			scans.provider.store(removed)
			_, err := scans.store.CleanUp(removed)
			// The walk runs on a goroutine of the startup read, so require cannot stop the test here.
			assert.NoError(t, err)
			return release.ParseEpisodeName(path)
		}

		scans.start(t)

		assert.Empty(t, scans.scanCounts(t, root))
		assert.Zero(t, scans.store.sourceFolderFiles([]string{root}))
	})
}

func TestScheduleSourceFolderScans_CountsTheIntervalFromTheEndOfALongStartupScan(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root := t.TempDir()
		writeSourceFile(t, root, keptEpisode)
		scans := newScheduledScans(folderConfig("15m", root))
		var once sync.Once
		scans.store.parseEpisodeName = func(path string) (*rls.Release, bool) {
			// The walk of a large library takes longer than the interval.
			once.Do(func() { time.Sleep(20 * time.Minute) })
			return release.ParseEpisodeName(path)
		}
		started := time.Now()
		scans.start(t)
		require.Equal(t, []int{1}, scans.scanCounts(t, root))
		require.Equal(t, 20*time.Minute, time.Since(started))

		// The bound fails a schedule that never scans, instead of a test that never ends.
		for len(scans.scanCounts(t, root)) < 2 && time.Since(started) < time.Hour {
			time.Sleep(time.Second)
			synctest.Wait()
		}

		assert.Equal(t, 35*time.Minute, time.Since(started).Truncate(time.Second),
			"the next scan starts one interval after the end of the startup scan")
	})
}
