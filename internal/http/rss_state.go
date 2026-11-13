// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"cmp"
	"crypto/sha256"
	"maps"
	"slices"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/prowlarr"
)

const (
	rssRetention  = 7 * 24 * time.Hour
	rssMaxEntries = 1024
	rssMaxBytes   = 4 << 20
)

type rssEntry struct {
	result  prowlarr.Result
	expires time.Time
	order   uint64
}

// RSS state is process-local and serialized by the discovery overlap guard.
// Checkpoints contain at most one page per selected indexer. Candidate storage
// has independent count, byte, and age limits. No acceptance decisions are stored.
type rssState struct {
	checkpoints map[int]map[searchMetadataKey]bool
	entries     map[searchMetadataKey]rssEntry
	bytes       int
	sequence    uint64
}

func rssIdentity(indexerID int, result prowlarr.Result) searchMetadataKey {
	key := metadataKey(indexerID, result)
	if key.indexerID == 0 {
		return searchMetadataKey{indexerID, sha256.Sum256([]byte(result.Title))}
	}
	return key
}

func (s rssState) clone() rssState {
	// Existing checkpoint sets are immutable; a poll replaces the entire set.
	s.checkpoints = maps.Clone(s.checkpoints)
	s.entries = maps.Clone(s.entries)
	return s
}

func (s *rssState) prune(indexers []prowlarr.Indexer, now time.Time) {
	if s.entries == nil {
		s.entries = make(map[searchMetadataKey]rssEntry)
		s.checkpoints = make(map[int]map[searchMetadataKey]bool)
	}
	eligible := func(id int) bool {
		return slices.ContainsFunc(indexers, func(i prowlarr.Indexer) bool { return i.ID == id })
	}
	maps.DeleteFunc(s.checkpoints, func(id int, _ map[searchMetadataKey]bool) bool { return !eligible(id) })
	for key, entry := range s.entries {
		if !now.Before(entry.expires) || !eligible(key.indexerID) {
			s.remove(key)
		}
	}
}

func rssResultSize(result prowlarr.Result) int {
	return len(result.Title) + len(result.GUID) + len(result.Link) + len(result.Enclosure.URL)
}

func (s *rssState) remember(key searchMetadataKey, result prowlarr.Result, now time.Time) {
	size := rssResultSize(result)
	if size > rssMaxBytes {
		return
	}
	entry, exists := s.entries[key]
	if exists {
		s.remove(key)
	} else {
		s.sequence++
		entry = rssEntry{expires: now.Add(rssRetention), order: s.sequence}
	}
	for len(s.entries) >= rssMaxEntries || s.bytes+size > rssMaxBytes {
		var oldest searchMetadataKey
		order := ^uint64(0)
		for k, e := range s.entries {
			if e.order < order {
				oldest, order = k, e.order
			}
		}
		s.remove(oldest)
	}
	entry.result = result
	s.entries[key] = entry
	s.bytes += size
}

func (s *rssState) remove(key searchMetadataKey) {
	s.bytes -= rssResultSize(s.entries[key].result)
	delete(s.entries, key)
}

func (s *rssState) results(indexerID int) []prowlarr.Result {
	var entries []rssEntry
	for key, entry := range s.entries {
		if key.indexerID == indexerID {
			entries = append(entries, entry)
		}
	}
	slices.SortFunc(entries, func(a, b rssEntry) int { return cmp.Compare(a.order, b.order) })
	results := make([]prowlarr.Result, len(entries))
	for i, entry := range entries {
		results[i] = entry.result
	}
	return results
}
