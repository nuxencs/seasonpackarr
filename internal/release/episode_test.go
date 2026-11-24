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
