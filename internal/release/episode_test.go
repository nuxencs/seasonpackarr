// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package release

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEpisodeFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "sample with dash", path: "test.release.s06e03.dutch.1080p.web.h264-rlsgrp-sample.mkv", want: false},
		{name: "sample with dot", path: "test.release.s06e03.dutch.1080p.web.h264-rlsgrp.sample.mkv", want: false},
		{name: "sample in uppercase", path: "test.release.s06e03.dutch.1080p.web.h264-rlsgrp.SAMPLE.mkv", want: false},
		{name: "sample as group suffix", path: "test.release.s06e03.dutch.1080p.web.h264-rlsgrp-sample.mkv", want: false},
		{name: "wrong ext", path: "test.release.s06e03.dutch.1080p.web.h264-rlsgrp.nfo", want: false},
		{name: "wrong ext and sample", path: "test.release.s06e03.dutch.1080p.web.h264-rlsgrp.sample.nfo", want: false},
		{name: "extra video", path: "Extras/Interview.1080p.WEB-DL.mkv", want: false},
		{name: "special episode", path: "test.release.s00e01.dutch.1080p.web.h264-rlsgrp.mkv", want: true},
		{name: "valid release", path: "test.release.s06e03.dutch.1080p.web.h264-rlsgrp.mkv", want: true},
		{name: "valid mp4 release", path: "test.release.s06e03.dutch.1080p.web.h264-rlsgrp.MP4", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, ok := ParseEpisodeFile(tt.path, 0)
			assert.Equalf(t, tt.want, ok, "ParseEpisodeFile(%q)", tt.path)
		})
	}
}

// TestParseEpisodeName fixes the result for the file name formats that the source folder guide names, so a parser
// update cannot change them without notice.
func TestParseEpisodeName(t *testing.T) {
	t.Parallel()

	type parsed struct {
		title      string
		year       int
		series     int
		episode    int
		resolution string
		source     string
		group      string
		hdr        []string
		service    string
	}
	tests := []struct {
		name string
		path string
		want *parsed
	}{
		{
			name: "scene style",
			path: "/data/tv/Series.Title.S01E02.1080p.WEB-DL.DDP5.1.H.264-GRP.mkv",
			want: &parsed{title: "Series Title", series: 1, episode: 2, resolution: "1080p", source: "WEB-DL", group: "GRP"},
		},
		{
			name: "TRaSH format",
			path: "/data/tv/Series Title (2023)/Season 01/Series Title (2023) - S01E02 - Episode Title [AMZN WEBDL-1080p][DV HDR10][EAC3 5.1][h265]-GRP.mkv",
			want: &parsed{title: "Series Title", year: 2023, series: 1, episode: 2, resolution: "1080p", source: "WEB-DL", group: "GRP", hdr: []string{"DV", "HDR10"}, service: "AMZN"},
		},
		{
			// The name has no release group, so the parser reads the resolution as the group.
			name: "default Sonarr format",
			path: "/data/tv/Series Title (2023)/Season 01/Series Title (2023) - S01E02 - Episode Title WEBDL-1080p.mkv",
			want: &parsed{title: "Series Title", year: 2023, series: 1, episode: 2, source: "WEB-DL", group: "1080p"},
		},
		{name: "obfuscated", path: "/data/tv/a3f9c1e07b2d4e8f9a0b.mkv"},
		{name: "sample", path: "/data/tv/Series.Title.S01E02.1080p.WEB-DL-GRP-sample.mkv"},
		{name: "no video container", path: "/data/tv/Series.Title.S01E02.1080p.WEB-DL-GRP.nfo"},
		{name: "no episode", path: "/data/tv/Extras/Interview.1080p.WEB-DL.mkv"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := ParseEpisodeName(tt.path)
			_, fileOK := ParseEpisodeFile(tt.path, 0)
			assert.Equal(t, fileOK, ok, "ParseEpisodeName and ParseEpisodeFile accept the same names")
			if tt.want == nil {
				assert.False(t, ok)
				return
			}
			require.True(t, ok)
			assert.Equal(t, *tt.want, parsed{
				title:      got.Title,
				year:       got.Year,
				series:     got.Series,
				episode:    got.Episode,
				resolution: got.Resolution,
				source:     got.Source,
				group:      got.Group,
				hdr:        got.HDR,
				service:    got.Collection,
			})
		})
	}
}

func TestEpisodeMatcher_ChecksEveryCompatibilityField(t *testing.T) {
	t.Parallel()

	target := requireEpisodeFile(t, "Show.S01/Show.S01E01.1080p.WEB-DL-GROUP.mkv", 100)
	sources := []EpisodeFile{
		requireEpisodeFile(t, "/client/Show.S01E01.1080p.WEB-DL-GROUP.mp4", 100),
		requireEpisodeFile(t, "/client/Show.S01E01.1080p.WEB-DL-GROUP.mkv", 101),
		requireEpisodeFile(t, "/client/Show.S02E01.1080p.WEB-DL-GROUP.mkv", 100),
		requireEpisodeFile(t, "/client/Show.S01E02.1080p.WEB-DL-GROUP.mkv", 100),
		requireEpisodeFile(t, "/client/Show.S01E01.2160p.WEB-DL-GROUP.mkv", 100),
		requireEpisodeFile(t, "/client/Show.S01E01.1080p.WEB-DL-OTHER.mkv", 100),
		requireEpisodeFile(t, "/client/Show.S01E01.1080p.WEB-DL-GROUP.mkv", 100),
	}

	result := NewEpisodeMatcher([]EpisodeFile{target}).Match(sources)
	require.Equal(t, []EpisodeMatch{{
		ClientPath:  "/client/Show.S01E01.1080p.WEB-DL-GROUP.mkv",
		TorrentPath: "Show.S01/Show.S01E01.1080p.WEB-DL-GROUP.mkv",
	}}, result.Matches)
}

func TestEpisodeMatcher_PreservesTargetOrderAndDeduplicatesSources(t *testing.T) {
	t.Parallel()

	targets := []EpisodeFile{
		requireEpisodeFile(t, "Show.S01/Show.S01E01.1080p.WEB-DL-GROUP.mkv", 100),
		requireEpisodeFile(t, "Show.S01/Show.S01E02.1080p.WEB-DL-GROUP.mkv", 200),
	}
	sources := []EpisodeFile{
		requireEpisodeFile(t, "/client/Show.S01E02.1080p.WEB-DL-GROUP.mkv", 200),
		requireEpisodeFile(t, "/first/Show.S01E01.1080p.WEB-DL-GROUP.mkv", 100),
		requireEpisodeFile(t, "/second/Show.S01E01.1080p.WEB-DL-GROUP.mkv", 100),
	}

	result := NewEpisodeMatcher(targets).Match(sources)
	require.Equal(t, []EpisodeMatch{
		{
			ClientPath:  "/first/Show.S01E01.1080p.WEB-DL-GROUP.mkv",
			TorrentPath: "Show.S01/Show.S01E01.1080p.WEB-DL-GROUP.mkv",
		},
		{
			ClientPath:  "/client/Show.S01E02.1080p.WEB-DL-GROUP.mkv",
			TorrentPath: "Show.S01/Show.S01E02.1080p.WEB-DL-GROUP.mkv",
		},
	}, result.Matches)
}

func TestEpisodeMatcher_KeepsFirstDuplicateTarget(t *testing.T) {
	t.Parallel()

	targets := []EpisodeFile{
		requireEpisodeFile(t, "first/Show.S01E01.1080p.WEB-DL-GROUP.mkv", 100),
		requireEpisodeFile(t, "second/Show.S01E01.1080p.WEB-DL-GROUP.mkv", 100),
	}
	sources := []EpisodeFile{
		requireEpisodeFile(t, "/client-a/Show.S01E01.1080p.WEB-DL-GROUP.mkv", 100),
		requireEpisodeFile(t, "/client-b/Show.S01E01.1080p.WEB-DL-GROUP.mkv", 100),
	}

	result := NewEpisodeMatcher(targets).Match(sources)
	require.Equal(t, []EpisodeMatch{{
		ClientPath:  "/client-a/Show.S01E01.1080p.WEB-DL-GROUP.mkv",
		TorrentPath: "first/Show.S01E01.1080p.WEB-DL-GROUP.mkv",
	}}, result.Matches)
	require.Equal(t, []EpisodeUnmatched{{
		TorrentPath: "second/Show.S01E01.1080p.WEB-DL-GROUP.mkv",
		Reason:      EpisodeUnmatchedDuplicateTarget,
	}}, result.Unmatched)
}

func TestEpisodeMatcher_ReportsTargetWithoutSourceEpisode(t *testing.T) {
	t.Parallel()

	targets := []EpisodeFile{
		requireEpisodeFile(t, "Show.S01/Show.S01E01.1080p.WEB-DL-GROUP.mkv", 100),
		requireEpisodeFile(t, "Show.S01/Show.S01E02.1080p.WEB-DL-GROUP.mkv", 200),
	}
	sources := []EpisodeFile{
		requireEpisodeFile(t, "/client/Show.S01E01.1080p.WEB-DL-GROUP.mkv", 100),
	}

	result := NewEpisodeMatcher(targets).Match(sources)
	require.Equal(t, []EpisodeUnmatched{{
		TorrentPath: "Show.S01/Show.S01E02.1080p.WEB-DL-GROUP.mkv",
		Reason:      EpisodeUnmatchedSourceMissing,
	}}, result.Unmatched)
}

func TestEpisodeMatcher_ReportsClosestCompatibilityMismatch(t *testing.T) {
	t.Parallel()

	target := requireEpisodeFile(t, "Show.S01/Show.S01E05.1080p.WEB-DL-GROUP.mkv", 100)
	sources := []EpisodeFile{
		requireEpisodeFile(t, "/client/Show.S01E05.1080p.WEB-DL-GROUP.mp4", 101),
		requireEpisodeFile(t, "/client/Show.S01E05.1080p.WEB-DL-OTHER.mkv", 100),
	}

	result := NewEpisodeMatcher([]EpisodeFile{target}).Match(sources)
	require.Equal(t, []EpisodeUnmatched{{
		TorrentPath:       "Show.S01/Show.S01E05.1080p.WEB-DL-GROUP.mkv",
		ClosestClientPath: "/client/Show.S01E05.1080p.WEB-DL-OTHER.mkv",
		Reason:            EpisodeUnmatchedCompatibilityMismatch,
		Mismatches: []Mismatch{{
			Field: MismatchReleaseGroup,
			Want:  "group",
			Got:   "other",
		}},
	}}, result.Unmatched)
}

func TestEpisodeMatcher_ReportsWantAndGotForEveryCompatibilityMismatch(t *testing.T) {
	t.Parallel()

	target := requireEpisodeFile(t, "Show.S01/Show.S01E05.1080p.WEB-DL-GROUP.mkv", 100)
	source := requireEpisodeFile(t, "/client/Show.S01E05.2160p.WEB-DL-OTHER.mp4", 101)

	result := NewEpisodeMatcher([]EpisodeFile{target}).Match([]EpisodeFile{source})
	require.Equal(t, []Mismatch{
		{Field: MismatchSize, Want: int64(100), Got: int64(101)},
		{Field: MismatchContainer, Want: "mkv", Got: "mp4"},
		{Field: MismatchResolution, Want: "1080p", Got: "2160p"},
		{Field: MismatchReleaseGroup, Want: "group", Got: "other"},
	}, result.Unmatched[0].Mismatches)
}

func TestEpisodeMatcher_MatchesFileNameAtAnyFolderDepth(t *testing.T) {
	t.Parallel()

	const targetPath = "Series Title 2022 S02/Test/Series Title 2022 S02E01 Test 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv"
	const sourcePath = "/data/torrents/tv/Test/Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv"
	target := requireEpisodeFile(t, targetPath, 2316560346)
	source := requireEpisodeFile(t, sourcePath, 2316560346)

	result := NewEpisodeMatcher([]EpisodeFile{target}).Match([]EpisodeFile{source})
	require.Equal(t, []EpisodeMatch{{ClientPath: sourcePath, TorrentPath: targetPath}}, result.Matches)
}

func TestEpisodeMatcher_IgnoresContainerLetterCase(t *testing.T) {
	t.Parallel()

	target := requireEpisodeFile(t, "Show.S01/Show.S01E01.1080p.WEB-DL-GROUP.MKV", 100)
	source := requireEpisodeFile(t, "/client/Show.S01E01.1080p.WEB-DL-GROUP.mkv", 100)

	result := NewEpisodeMatcher([]EpisodeFile{target}).Match([]EpisodeFile{source})
	require.Equal(t, []EpisodeMatch{{
		ClientPath:  "/client/Show.S01E01.1080p.WEB-DL-GROUP.mkv",
		TorrentPath: "Show.S01/Show.S01E01.1080p.WEB-DL-GROUP.MKV",
	}}, result.Matches)
}

func BenchmarkEpisodeMatching(b *testing.B) {
	for _, episodeCount := range []int{12, 24, 100} {
		b.Run(fmt.Sprintf("episodes_%d", episodeCount), func(b *testing.B) {
			clientPaths, torrentPaths := benchmarkEpisodePaths(episodeCount)
			b.ResetTimer()
			for b.Loop() {
				targets := make([]EpisodeFile, 0, episodeCount)
				for _, path := range torrentPaths {
					target, _ := ParseEpisodeFile(path, 2_000_000_000)
					targets = append(targets, target)
				}
				matcher := NewEpisodeMatcher(targets)

				sources := make([]EpisodeFile, 0, episodeCount)
				for _, path := range clientPaths {
					source, _ := ParseEpisodeFile(path, 2_000_000_000)
					sources = append(sources, source)
				}
				_ = matcher.Match(sources)
			}
		})
	}
}

func requireEpisodeFile(t *testing.T, path string, size int64) EpisodeFile {
	t.Helper()
	episode, ok := ParseEpisodeFile(path, size)
	require.True(t, ok, "ParseEpisodeFile(%q)", path)
	return episode
}

func benchmarkEpisodePaths(episodeCount int) ([]string, []string) {
	clientPaths := make([]string, episodeCount)
	torrentPaths := make([]string, episodeCount)
	for index := range episodeCount {
		clientPaths[index] = fmt.Sprintf("/data/tv/Show.S01E%02d.1080p.WEB-DL.DDP5.1.H.264-GROUP.mkv", index+1)
		torrentPaths[index] = fmt.Sprintf("Show.S01/Show.S01E%02d.1080p.WEB-DL.DDP5.1.H.264-GROUP.mkv", index+1)
	}
	return clientPaths, torrentPaths
}
