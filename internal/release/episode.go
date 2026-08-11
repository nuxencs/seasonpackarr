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

// EpisodeMatch maps one reusable client episode to one announced torrent
// target.
type EpisodeMatch struct {
	ClientPath  string
	TorrentPath string
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
func (m EpisodeMatcher) Match(sources []EpisodeFile) []EpisodeMatch {
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
	for targetIndex, target := range m.targets {
		clientPath, ok := clientPathByTarget[targetIndex]
		if !ok {
			continue
		}
		matches = append(matches, EpisodeMatch{
			ClientPath:  clientPath,
			TorrentPath: target.path,
		})
	}
	return matches
}
