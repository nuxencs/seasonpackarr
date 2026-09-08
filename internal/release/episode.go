// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package release

import (
	"path/filepath"
	"strings"

	"github.com/autobrr/rls"
)

type episodeMatchKey struct {
	size       int64
	ext        string
	series     int
	episode    int
	resolution string
	group      string
}

type episodeIdentityKey struct {
	series  int
	episode int
}

// EpisodeFile is a valid parsed episode video. Its parser details stay private
// so callers cannot construct a partially normalized matching value.
type EpisodeFile struct {
	path string
	key  episodeMatchKey
}

// ParseEpisodeFile validates and parses one episode video for indexed matching.
// It checks the container before invoking the release parser so obvious
// non-video files stay on the cheap path.
func ParseEpisodeFile(path string, size int64) (EpisodeFile, bool) {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if ext != "mkv" && ext != "mp4" {
		return EpisodeFile{}, false
	}

	parsed := parseEpisodeFile(path)
	if parsed.Type != rls.Episode && len(parsed.SeriesEpisodes()) == 0 {
		return EpisodeFile{}, false
	}

	group := rls.MustNormalize(parsed.Group)
	if group == "sample" {
		return EpisodeFile{}, false
	}

	return EpisodeFile{
		path: path,
		key: episodeMatchKey{
			size:       size,
			ext:        ext,
			series:     parsed.Series,
			episode:    parsed.Episode,
			resolution: parsed.Resolution,
			group:      group,
		},
	}, true
}

// Path returns the original path supplied to ParseEpisodeFile.
func (f EpisodeFile) Path() string {
	return f.path
}

// Size returns the declared episode size in bytes.
func (f EpisodeFile) Size() int64 { return f.key.size }

// EpisodeMatch maps one reusable client episode to one announced torrent
// target.
type EpisodeMatch struct {
	ClientPath  string
	TorrentPath string
}

type EpisodeUnmatchedReason string

const (
	EpisodeUnmatchedSourceMissing         EpisodeUnmatchedReason = "source_episode_not_found"
	EpisodeUnmatchedCompatibilityMismatch EpisodeUnmatchedReason = "compatibility_mismatch"
	EpisodeUnmatchedDuplicateTarget       EpisodeUnmatchedReason = "duplicate_torrent_target"
)

type EpisodeMismatchField string

const (
	EpisodeMismatchSize         EpisodeMismatchField = "size"
	EpisodeMismatchContainer    EpisodeMismatchField = "container"
	EpisodeMismatchResolution   EpisodeMismatchField = "resolution"
	EpisodeMismatchReleaseGroup EpisodeMismatchField = "release_group"
)

const episodeCompatibilityFieldCount = 4

// EpisodeMismatch reports one incompatible value. Want is the announced
// torrent target requirement. Got is the closest client source value.
type EpisodeMismatch struct {
	Field EpisodeMismatchField
	Want  any
	Got   any
}

// EpisodeUnmatched explains why one valid torrent target cannot reuse a client
// episode.
type EpisodeUnmatched struct {
	TorrentPath       string
	ClosestClientPath string
	Reason            EpisodeUnmatchedReason
	Mismatches        []EpisodeMismatch
}

// EpisodeMatchResult contains the reusable links and diagnostics for every
// valid torrent target that did not receive one.
type EpisodeMatchResult struct {
	Matches   []EpisodeMatch
	Unmatched []EpisodeUnmatched
}

// EpisodeMatcher indexes valid torrent targets once. Duplicate target keys keep
// the first target, matching the former stable nested-scan behavior.
type EpisodeMatcher struct {
	targets          []EpisodeFile
	firstTargetByKey map[episodeMatchKey]int
}

func NewEpisodeMatcher(targets []EpisodeFile) EpisodeMatcher {
	matcher := EpisodeMatcher{
		targets:          targets,
		firstTargetByKey: make(map[episodeMatchKey]int, len(targets)),
	}
	for index, target := range targets {
		if _, exists := matcher.firstTargetByKey[target.key]; !exists {
			matcher.firstTargetByKey[target.key] = index
		}
	}
	return matcher
}

func (m EpisodeMatcher) Len() int {
	return len(m.targets)
}

// Match returns at most one client source per torrent target. Results follow
// torrent target order, independent of client inventory order.
func (m EpisodeMatcher) Match(sources []EpisodeFile) EpisodeMatchResult {
	clientPathByTarget := make(map[int]string, min(len(sources), len(m.targets)))
	for _, source := range sources {
		targetIndex, ok := m.firstTargetByKey[source.key]
		if !ok {
			continue
		}
		if _, exists := clientPathByTarget[targetIndex]; !exists {
			clientPathByTarget[targetIndex] = source.path
		}
	}

	matches := make([]EpisodeMatch, 0, len(clientPathByTarget))
	unmatched := make([]EpisodeUnmatched, 0, len(m.targets)-len(clientPathByTarget))
	var sourcesByIdentity map[episodeIdentityKey][]EpisodeFile
	if len(clientPathByTarget) < len(m.targets) {
		sourcesByIdentity = make(map[episodeIdentityKey][]EpisodeFile, len(sources))
		for _, source := range sources {
			identity := episodeIdentityKey{series: source.key.series, episode: source.key.episode}
			sourcesByIdentity[identity] = append(sourcesByIdentity[identity], source)
		}
	}
	for targetIndex, target := range m.targets {
		clientPath, ok := clientPathByTarget[targetIndex]
		if !ok {
			if firstTarget := m.firstTargetByKey[target.key]; firstTarget != targetIndex {
				unmatched = append(unmatched, EpisodeUnmatched{
					TorrentPath: target.path,
					Reason:      EpisodeUnmatchedDuplicateTarget,
				})
				continue
			}
			identity := episodeIdentityKey{series: target.key.series, episode: target.key.episode}
			unmatched = append(unmatched, diagnoseUnmatchedEpisode(target, sourcesByIdentity[identity]))
			continue
		}
		matches = append(matches, EpisodeMatch{
			ClientPath:  clientPath,
			TorrentPath: target.path,
		})
	}
	return EpisodeMatchResult{Matches: matches, Unmatched: unmatched}
}

func diagnoseUnmatchedEpisode(target EpisodeFile, sources []EpisodeFile) EpisodeUnmatched {
	diagnostic := EpisodeUnmatched{
		TorrentPath: target.path,
		Reason:      EpisodeUnmatchedSourceMissing,
	}
	bestFieldCount := episodeCompatibilityFieldCount + 1
	for _, source := range sources {
		mismatches := compatibilityMismatches(source.key, target.key)
		if len(mismatches) == 0 || len(mismatches) >= bestFieldCount {
			continue
		}
		bestFieldCount = len(mismatches)
		diagnostic.ClosestClientPath = source.path
		diagnostic.Reason = EpisodeUnmatchedCompatibilityMismatch
		diagnostic.Mismatches = mismatches
	}
	return diagnostic
}

func compatibilityMismatches(source, target episodeMatchKey) []EpisodeMismatch {
	mismatches := make([]EpisodeMismatch, 0, episodeCompatibilityFieldCount)
	if source.size != target.size {
		mismatches = append(mismatches, EpisodeMismatch{
			Field: EpisodeMismatchSize,
			Want:  target.size,
			Got:   source.size,
		})
	}
	if source.ext != target.ext {
		mismatches = append(mismatches, EpisodeMismatch{
			Field: EpisodeMismatchContainer,
			Want:  target.ext,
			Got:   source.ext,
		})
	}
	if source.resolution != target.resolution {
		mismatches = append(mismatches, EpisodeMismatch{
			Field: EpisodeMismatchResolution,
			Want:  target.resolution,
			Got:   source.resolution,
		})
	}
	if source.group != target.group {
		mismatches = append(mismatches, EpisodeMismatch{
			Field: EpisodeMismatchReleaseGroup,
			Want:  target.group,
			Got:   source.group,
		})
	}
	return mismatches
}
