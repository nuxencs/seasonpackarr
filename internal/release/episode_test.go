// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package release

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEpisodeMatcherChecksEveryCompatibilityField(t *testing.T) {
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

	matches := NewEpisodeMatcher([]EpisodeFile{target}).Match(sources)
	require.Equal(t, []EpisodeMatch{{
		ClientPath:  "/client/Show.S01E01.1080p.WEB-DL-GROUP.mkv",
		TorrentPath: "Show.S01/Show.S01E01.1080p.WEB-DL-GROUP.mkv",
	}}, matches)
}

func TestEpisodeMatcherPreservesTargetOrderAndDeduplicatesSources(t *testing.T) {
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

	matches := NewEpisodeMatcher(targets).Match(sources)
	require.Equal(t, []EpisodeMatch{
		{
			ClientPath:  "/first/Show.S01E01.1080p.WEB-DL-GROUP.mkv",
			TorrentPath: "Show.S01/Show.S01E01.1080p.WEB-DL-GROUP.mkv",
		},
		{
			ClientPath:  "/client/Show.S01E02.1080p.WEB-DL-GROUP.mkv",
			TorrentPath: "Show.S01/Show.S01E02.1080p.WEB-DL-GROUP.mkv",
		},
	}, matches)
}

func TestEpisodeMatcherKeepsFirstDuplicateTarget(t *testing.T) {
	t.Parallel()

	targets := []EpisodeFile{
		requireEpisodeFile(t, "first/Show.S01E01.1080p.WEB-DL-GROUP.mkv", 100),
		requireEpisodeFile(t, "second/Show.S01E01.1080p.WEB-DL-GROUP.mkv", 100),
	}
	sources := []EpisodeFile{
		requireEpisodeFile(t, "/client-a/Show.S01E01.1080p.WEB-DL-GROUP.mkv", 100),
		requireEpisodeFile(t, "/client-b/Show.S01E01.1080p.WEB-DL-GROUP.mkv", 100),
	}

	matches := NewEpisodeMatcher(targets).Match(sources)
	require.Equal(t, []EpisodeMatch{{
		ClientPath:  "/client-a/Show.S01E01.1080p.WEB-DL-GROUP.mkv",
		TorrentPath: "first/Show.S01E01.1080p.WEB-DL-GROUP.mkv",
	}}, matches)
}

func BenchmarkEpisodeMatching(b *testing.B) {
	for _, episodeCount := range []int{12, 24, 100} {
		b.Run(fmt.Sprintf("nested_%d", episodeCount), func(b *testing.B) {
			clientPaths, torrentPaths := benchmarkEpisodePaths(episodeCount)
			b.ResetTimer()
			for b.Loop() {
				for _, clientPath := range clientPaths {
					for _, torrentPath := range torrentPaths {
						matched, _ := MatchEpToSeasonPackEp(clientPath, 2_000_000_000, torrentPath, 2_000_000_000)
						if matched != "" {
							break
						}
					}
				}
			}
		})

		b.Run(fmt.Sprintf("indexed_%d", episodeCount), func(b *testing.B) {
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
