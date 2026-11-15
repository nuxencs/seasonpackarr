// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package seasonpack

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient/torrentclienttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatch_UsesTorrentEpisodeCoverage(t *testing.T) {
	pack := newPack(t, "Coverage", 12, 10)
	f := newFixture(pack.client)

	outcome := f.processor.Match(t.Context(), f.client(t, "default", newSmartConfig(0.75)), pack.name, TorrentBytes(pack.torrent))

	require.NoError(t, outcome.Err)
	require.Equal(t, domain.StatusSuccessfulMatch, outcome.Status)
	require.Equal(t, &Coverage{Reusable: 10, Total: 12}, outcome.Coverage)
	require.False(t, pack.client.ImportCalled, "match evaluation must remain side-effect free")
	require.Equal(t, 1, pack.client.FileBatchCalls, "one plan must use one bulk file read")
}

// A rejected match keeps no import plan: the import with a lower threshold
// reads the file details again.
func TestMatch_RejectsCoverageBelowTorrentThreshold(t *testing.T) {
	pack := newPack(t, "BelowThreshold", 4, 2)
	f := newFixture(pack.client)

	outcome := f.processor.Match(t.Context(), f.client(t, "default", newSmartConfig(0.75)), pack.name, TorrentBytes(pack.torrent))

	require.EqualError(t, outcome.Err, domain.StatusBelowThreshold.String())
	require.Equal(t, domain.StatusBelowThreshold, outcome.Status)
	require.Equal(t, Rejected, outcome.Class())
	require.Equal(t, &Coverage{Reusable: 2, Total: 4}, outcome.Coverage)
	require.False(t, pack.client.ImportCalled)
	require.Equal(t, 1, pack.client.FileBatchCalls)

	outcome = f.processor.Import(t.Context(), f.client(t, "default", newSmartConfig(0.5)), pack.name, pack.torrent)
	require.NoError(t, outcome.Err)
	require.Equal(t, 2, pack.client.FileBatchCalls, "the import must build the plan, because the match stored none")
}

// TestMatch_ChangesNothing locks in that the match has no side effects: it
// accepts the pack without an import call and without a hardlink.
func TestMatch_ChangesNothing(t *testing.T) {
	pack := newPack(t, "Series", 1, 1)
	f := newFixture(pack.client)

	outcome := f.processor.Match(t.Context(), f.defaultClient(t), pack.name, TorrentBytes(pack.torrent))

	require.NoError(t, outcome.Err)
	require.Equal(t, domain.StatusSuccessfulMatch, outcome.Status)
	require.False(t, pack.client.ImportCalled, "the match must not import")
	require.NoFileExists(t, pack.target(1))
	entries, err := os.ReadDir(pack.importDir)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestMatch_CoverageCannotExceedTorrentEpisodeCount(t *testing.T) {
	pack := newPack(t, "CappedCoverage", 10, 10)
	short := newPack(t, "CappedCoverage", 6, 0)
	f := newFixture(pack.client)

	outcome := f.processor.Match(t.Context(), f.defaultClient(t), short.name, TorrentBytes(short.torrent))

	require.NoError(t, outcome.Err)
	require.Equal(t, domain.StatusSuccessfulMatch, outcome.Status)
	require.Equal(t, &Coverage{Reusable: 6, Total: 6}, outcome.Coverage, "client episodes outside the torrent cannot increase coverage")
}

// With the torrent in hand, one failed file-detail read does not lose the pack.
func TestMatch_ContinuesAfterFileDetailReadErrorWithBytesInHand(t *testing.T) {
	pack := newPack(t, "Show", 2, 2)
	pack.client.FileErrByHash = map[string]error{"ep2": errors.New("boom")}
	f := newFixture(pack.client)

	outcome := f.processor.Match(t.Context(), f.defaultClient(t), pack.name, TorrentBytes(pack.torrent))

	require.NoError(t, outcome.Err)
	require.Equal(t, &Coverage{Reusable: 1, Total: 2}, outcome.Coverage)
	require.Equal(t, 1, pack.client.FileBatchCalls)
	require.Equal(t, 2, pack.client.FileCalls)
	require.ElementsMatch(t, []string{"ep1", "ep2"}, pack.client.GotHashes)
	f.log.Events(t).Require(t, "error getting file info: "+pack.episodeRelease(2))
}

func TestMatch_RejectsPackWhenNoSourceIsOnDisk(t *testing.T) {
	pack := newPack(t, "Missing", 2, 2)
	require.NoError(t, os.Remove(pack.source(1)))
	require.NoError(t, os.Remove(pack.source(2)))
	f := newFixture(pack.client)

	outcome := f.processor.Match(t.Context(), f.defaultClient(t), pack.name, TorrentBytes(pack.torrent))

	require.Equal(t, domain.StatusFailedMatchToTorrentEps, outcome.Status)
	require.EqualError(t, outcome.Err, domain.StatusFailedMatchToTorrentEps.String())
	require.Equal(t, Rejected, outcome.Class())
	require.Equal(t, &Coverage{Reusable: 0, Total: 2}, outcome.Coverage)
	dropped := f.log.Events(t).RequireField(t, "client episode file is not reusable", "source", pack.source(1))
	assert.Equal(t, "missing", dropped["reason"])
}

// The client moves episodes between the candidate check and the match. The
// match finds each file at its new path, with no second inventory read.
func TestMatch_FindsEpisodeMovedAfterCandidateCheck(t *testing.T) {
	tests := []struct {
		name  string
		cfg   domain.Config
		moved []int
	}{
		{name: "smart mode at 100 percent", cfg: newSmartConfig(1), moved: []int{2}},
		{name: "no smart mode", cfg: newConfig(), moved: []int{1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pack := newPack(t, "Moved", 2, 2)
			f := newFixture(pack.client)
			client := f.client(t, "default", tt.cfg)
			require.NoError(t, f.processor.Candidate(t.Context(), client, pack.name).Err)
			for _, episode := range tt.moved {
				pack.moveEpisode(t, episode)
			}

			outcome := f.processor.Match(t.Context(), client, pack.name, TorrentBytes(pack.torrent))

			require.NoError(t, outcome.Err)
			assert.Equal(t, domain.StatusSuccessfulMatch, outcome.Status)
			assert.Equal(t, &Coverage{Reusable: 2, Total: 2}, outcome.Coverage)
			assert.Equal(t, 1, pack.client.TorrentCalls, "the match must need no inventory refresh")
		})
	}
}

// The request checks of the match read no client.
func TestMatch_ChecksRequestBeforeClientRead(t *testing.T) {
	pack := newPack(t, "Checks", 1, 1)
	tests := []struct {
		name    string
		release string
		torrent Torrent
		want    domain.StatusCode
	}{
		{name: "empty torrent bytes", release: pack.name, torrent: TorrentBytes(nil), want: domain.StatusTorrentBytesError},
		{name: "empty torrent bytes and empty release name", torrent: TorrentBytes(nil), want: domain.StatusTorrentBytesError},
		{name: "empty release name", torrent: TorrentBytes(pack.torrent), want: domain.StatusAnnounceNameError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(pack.client)

			outcome := f.processor.Match(t.Context(), f.defaultClient(t), tt.release, tt.torrent)

			assert.Equal(t, tt.want, outcome.Status)
			assert.EqualError(t, outcome.Err, tt.want.String())
			assert.Equal(t, Failed, outcome.Class())
			assert.Nil(t, outcome.Coverage)
			assert.Zero(t, pack.client.TorrentCalls)
		})
	}
}

// A fetch can cost a tracker request, so the match calls it only when it can use the torrent.
func TestMatch_FetchesTorrentOnlyAfterSourceCheck(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(t *testing.T, pack packFixture)
		release    func(pack packFixture) string
		wantStatus domain.StatusCode
		wantErr    error
		wantClass  Class
		wantFetch  int
	}{
		{
			name:       "candidate check rejects",
			release:    func(packFixture) string { return "Other.S01.1080p.WEB-DL.H.264-RlsGrp" },
			wantStatus: domain.StatusNoMatches,
			wantClass:  Rejected,
		},
		{
			name: "file-detail read fails",
			mutate: func(_ *testing.T, pack packFixture) {
				pack.client.FileErrByHash = map[string]error{"ep2": errors.New("boom")}
			},
			wantStatus: domain.StatusGetTorrentsError,
			wantErr:    ErrFileDetailRead,
			wantClass:  Failed,
		},
		{
			name: "no source on disk",
			mutate: func(t *testing.T, pack packFixture) {
				require.NoError(t, os.Remove(pack.source(1)))
				require.NoError(t, os.Remove(pack.source(2)))
			},
			wantStatus: domain.StatusFailedMatchToTorrentEps,
			wantErr:    ErrNoSourceOnDisk,
			wantClass:  Rejected,
		},
		{
			name:       "one source on disk",
			mutate:     func(t *testing.T, pack packFixture) { require.NoError(t, os.Remove(pack.source(2))) },
			wantStatus: domain.StatusSuccessfulMatch,
			wantClass:  Accepted,
			wantFetch:  1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pack := newPack(t, "Fetch", 2, 2)
			if tt.mutate != nil {
				tt.mutate(t, pack)
			}
			releaseName := pack.name
			if tt.release != nil {
				releaseName = tt.release(pack)
			}
			f := newFixture(pack.client)
			fetches := 0
			fetch := func(context.Context) ([]byte, error) {
				fetches++
				return pack.torrent, nil
			}

			outcome := f.processor.Match(t.Context(), f.defaultClient(t), releaseName, TorrentFetch(fetch))

			assert.Equal(t, tt.wantStatus, outcome.Status)
			assert.Equal(t, tt.wantClass, outcome.Class())
			assert.Equal(t, tt.wantFetch, fetches)
			if tt.wantErr != nil {
				assert.ErrorIs(t, outcome.Err, tt.wantErr)
			}
			if tt.wantFetch == 0 {
				assert.Nil(t, outcome.Coverage, "no torrent was read")
			} else {
				assert.Equal(t, &Coverage{Reusable: 1, Total: 2}, outcome.Coverage)
			}
			assert.False(t, pack.client.ImportCalled)
		})
	}
}

func TestMatch_ReturnsErrorOfFailedFetch(t *testing.T) {
	pack := newPack(t, "FetchError", 1, 1)
	f := newFixture(pack.client)
	fetchErr := errors.New("tracker is down")

	outcome := f.processor.Match(t.Context(), f.defaultClient(t), pack.name, TorrentFetch(func(context.Context) ([]byte, error) {
		return nil, fetchErr
	}))

	require.ErrorIs(t, outcome.Err, fetchErr)
	require.Equal(t, domain.StatusTorrentBytesError, outcome.Status)
	require.Equal(t, Failed, outcome.Class())
	require.Nil(t, outcome.Coverage)
}

// TestEpisodeFileFromFiles_FollowsTorrentClientPathContract locks in the load-bearing contract documented on
// torrentclient.TorrentClient: the hardlink source the processor uses is
// filepath.Join(FileResult.SavePath, <file name from the same result>). File
// names keep their torrent-root-folder prefix and SavePath is the absolute
// on-disk download dir, so the parsed episode path is the real file path. A
// future TorrentClient implementation that returns a bare basename or a
// non-absolute SavePath would break hardlinking silently. This test fails first.
func TestEpisodeFileFromFiles_FollowsTorrentClientPathContract(t *testing.T) {
	torrentClient := &torrentclienttest.Client{
		Torrents: []torrentclient.Torrent{{Hash: "abc123", Name: "Some.Show.S01.1080p"}},
		SavePath: "/data/torrents/tv",
		Files: []torrentclient.File{
			{Name: "Some.Show.S01.1080p/Some.Show.S01E01.1080p.mkv", Size: 1_000_000},
		},
	}
	ts, err := torrentClient.GetTorrents(t.Context())
	require.NoError(t, err)
	require.Len(t, ts, 1)

	results := torrentClient.GetFiles(t.Context(), []string{ts[0].Hash})
	require.Len(t, results, 1)
	require.NoError(t, results[0].Err)
	episode, err := episodeFileFromFiles(results[0].Files, results[0].SavePath)
	require.NoError(t, err)
	require.Equal(t, "abc123", torrentClient.GotHash)
	require.Equal(t, "/data/torrents/tv/Some.Show.S01.1080p/Some.Show.S01E01.1080p.mkv", episode.Path(), "hardlink source")
}

func TestEpisodeFileFromFiles_SelectsFirstValidEpisode(t *testing.T) {
	torrentClient := &torrentclienttest.Client{
		Files: []torrentclient.File{
			{Name: "Some.Show.S01/poster.jpg", Size: 50},                  // not a video
			{Name: "Some.Show.S01/Some.Show.S01E01.mkv", Size: 1_000_000}, // first valid
			{Name: "Some.Show.S01/Some.Show.S01E02.mkv", Size: 1_100_000},
		},
	}
	episode, err := episodeFileFromFiles(torrentClient.Files, "")
	require.NoError(t, err)
	require.Equal(t, "Some.Show.S01/Some.Show.S01E01.mkv", episode.Path())
}

func TestEpisodeFileFromFiles_ErrorsWhenNoValidEpisode(t *testing.T) {
	torrentClient := &torrentclienttest.Client{
		Files: []torrentclient.File{
			{Name: "Some.Show.S01/poster.jpg", Size: 50},
			{Name: "Some.Show.S01/readme.txt", Size: 10},
		},
	}
	_, err := episodeFileFromFiles(torrentClient.Files, "")
	require.Error(t, err)
}
