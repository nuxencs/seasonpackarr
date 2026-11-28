// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package state

import (
	"context"
	"database/sql"
	"time"
)

// searchTimeTTL is the age at which a stored search time expires. An expired
// time means "never searched", so rows of removed clients and indexers go away.
const searchTimeTTL = 90 * 24 * time.Hour

// SearchGroup is the key of a search group: the normalized show title and the
// season number, with no year.
type SearchGroup struct {
	Title  string
	Season int
}

// SearchTarget is one search group for one client on one indexer.
type SearchTarget struct {
	Group     SearchGroup
	Client    string
	IndexerID int
}

// SearchTimes returns the stored search times that did not expire at now.
func (s *Store) SearchTimes(ctx context.Context, now time.Time) (map[SearchTarget]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT title, season, client, indexer_id, searched_ns FROM search_times WHERE searched_ns > ?",
		now.Add(-searchTimeTTL).UnixNano())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	times := make(map[SearchTarget]time.Time)
	for rows.Next() {
		var target SearchTarget
		var searched int64
		if err := rows.Scan(&target.Group.Title, &target.Group.Season, &target.Client, &target.IndexerID, &searched); err != nil {
			return nil, err
		}
		times[target] = time.Unix(0, searched)
	}
	return times, rows.Err()
}

// SetSearchTimes stores at as the search time of the group on the indexer for
// each client, in one transaction.
func (s *Store) SetSearchTimes(ctx context.Context, group SearchGroup, indexerID int, clients []string, at time.Time) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		for _, client := range clients {
			if _, err := tx.ExecContext(ctx, `INSERT INTO search_times VALUES (?, ?, ?, ?, ?)
				ON CONFLICT(title, season, client, indexer_id) DO UPDATE SET searched_ns = excluded.searched_ns`,
				group.Title, group.Season, client, indexerID, at.UnixNano()); err != nil {
				return err
			}
		}
		return nil
	})
}
