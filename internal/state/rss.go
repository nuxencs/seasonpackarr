// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package state

import (
	"cmp"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
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
	release prowlarr.Release
	expires time.Time
	order   uint64
}

// Feeds is a per-run working set. SaveFeeds commits retained listings and
// checkpoints together. Discard the working set if client inventory is incomplete.
type Feeds struct {
	checkpoints map[int]map[Key]bool
	entries     map[Key]rssEntry
	bytes       int
	sequence    uint64
}

func RSSIdentity(indexerID int, release prowlarr.Release) Key {
	key := TorrentCacheKey(indexerID, release)
	if key.indexerID == 0 {
		return Key{indexerID, sha256.Sum256([]byte(release.Title))}
	}
	return key
}

func (s *Feeds) Checkpoint(indexerID int) map[Key]bool { return s.checkpoints[indexerID] }

func (s *Feeds) SetCheckpoint(indexerID int, page map[Key]bool) {
	s.checkpoints[indexerID] = page
}

func (s *Feeds) Prune(indexers []prowlarr.Indexer, now time.Time) {
	if s.entries == nil {
		s.entries = make(map[Key]rssEntry)
		s.checkpoints = make(map[int]map[Key]bool)
	}
	eligible := func(id int) bool {
		return slices.ContainsFunc(indexers, func(i prowlarr.Indexer) bool { return i.ID == id })
	}
	for id := range s.checkpoints {
		if !eligible(id) {
			delete(s.checkpoints, id)
		}
	}
	for key, entry := range s.entries {
		if !now.Before(entry.expires) || !eligible(key.indexerID) {
			s.remove(key)
		}
	}
}

func rssReleaseSize(release prowlarr.Release) int {
	return len(release.Title) + len(release.GUID) + len(release.Link) + len(release.Enclosure.URL)
}

func (s *Feeds) Remember(key Key, release prowlarr.Release, now time.Time) {
	size := rssReleaseSize(release)
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
		var oldest Key
		order := ^uint64(0)
		for k, e := range s.entries {
			if e.order < order {
				oldest, order = k, e.order
			}
		}
		s.remove(oldest)
	}
	entry.release = release
	s.entries[key] = entry
	s.bytes += size
}

func (s *Feeds) remove(key Key) {
	s.bytes -= rssReleaseSize(s.entries[key].release)
	delete(s.entries, key)
}

// Retained returns the retained listings of the indexer, oldest first.
func (s *Feeds) Retained(indexerID int) []prowlarr.Release {
	var entries []rssEntry
	for key, entry := range s.entries {
		if key.indexerID == indexerID {
			entries = append(entries, entry)
		}
	}
	slices.SortFunc(entries, func(a, b rssEntry) int { return cmp.Compare(a.order, b.order) })
	releases := make([]prowlarr.Release, len(entries))
	for i, entry := range entries {
		releases[i] = entry.release
	}
	return releases
}

func (s *Store) Feeds(ctx context.Context) (*Feeds, error) {
	feeds := &Feeds{entries: make(map[Key]rssEntry), checkpoints: make(map[int]map[Key]bool)}
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT indexer_id, identity FROM rss_checkpoints")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var key Key
			var identity []byte
			if err := rows.Scan(&key.indexerID, &identity); err != nil {
				return err
			}
			copy(key.identity[:], identity)
			if feeds.checkpoints[key.indexerID] == nil {
				feeds.checkpoints[key.indexerID] = make(map[Key]bool)
			}
			feeds.checkpoints[key.indexerID][key] = true
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		rows, err = tx.QueryContext(ctx, "SELECT indexer_id, identity, result, expires_ns, sequence FROM rss_candidates")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var key Key
			var identity, release []byte
			var expires int64
			var entry rssEntry
			if err := rows.Scan(&key.indexerID, &identity, &release, &expires, &entry.order); err != nil {
				return err
			}
			copy(key.identity[:], identity)
			if err := json.Unmarshal(release, &entry.release); err != nil {
				return err
			}
			entry.expires = time.Unix(0, expires)
			feeds.entries[key] = entry
			feeds.sequence = max(feeds.sequence, entry.order)
			feeds.bytes += rssReleaseSize(entry.release)
		}
		return rows.Err()
	})
	return feeds, err
}

// SaveFeeds advances checkpoints atomically with their retained listings.
func (s *Store) SaveFeeds(ctx context.Context, feeds *Feeds) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM rss_checkpoints; DELETE FROM rss_candidates"); err != nil {
			return err
		}
		for id, checkpoint := range feeds.checkpoints {
			for key := range checkpoint {
				if _, err := tx.ExecContext(ctx, "INSERT INTO rss_checkpoints VALUES (?, ?)", id, key.identity[:]); err != nil {
					return err
				}
			}
		}
		for key, entry := range feeds.entries {
			release, err := json.Marshal(entry.release)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO rss_candidates VALUES (?, ?, ?, ?, ?)",
				key.indexerID, key.identity[:], release, entry.expires.UnixNano(), entry.order); err != nil {
				return err
			}
		}
		return nil
	})
}
