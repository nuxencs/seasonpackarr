// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package state

import (
	"cmp"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/prowlarr"
)

const (
	metadataTTL        = 7 * 24 * time.Hour
	metadataMaxBytes   = 64 << 20
	metadataMaxEntries = 1024
)

// Key identifies metadata within one Prowlarr connection, without retaining a
// second copy of a potentially large or credential-bearing result URL.
type Key struct {
	indexerID int
	identity  [sha256.Size]byte
}

func MetadataKey(indexerID int, result prowlarr.Result) Key {
	identity := cmp.Or(result.GUID, result.Link, result.Enclosure.URL)
	if identity == "" {
		return Key{}
	}
	encoded, _ := json.Marshal([2]string{identity, result.Title})
	return Key{indexerID, sha256.Sum256(encoded)}
}

func (s *Store) Metadata(ctx context.Context, key Key, now time.Time) ([]byte, error) {
	var data []byte
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM metadata WHERE expires_ns <= ?", now.UnixNano()); err != nil {
			return err
		}
		err := tx.QueryRowContext(ctx, `UPDATE metadata SET used_ns = ? WHERE indexer_id = ? AND identity = ? RETURNING data`,
			now.UnixNano(), key.indexerID, key.identity[:]).Scan(&data)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	})
	return data, err
}

// PutMetadata stores validated bytes, not coverage or acceptance decisions.
func (s *Store) PutMetadata(ctx context.Context, key Key, data []byte, now time.Time) error {
	if key.indexerID <= 0 || len(data) == 0 || len(data) > metadataMaxBytes {
		return nil
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM metadata WHERE expires_ns <= ? OR (indexer_id = ? AND identity = ?)",
			now.UnixNano(), key.indexerID, key.identity[:]); err != nil {
			return err
		}
		var count, size int
		if err := tx.QueryRowContext(ctx, "SELECT count(*), coalesce(sum(length(data)), 0) FROM metadata").Scan(&count, &size); err != nil {
			return err
		}
		for count >= metadataMaxEntries || size+len(data) > metadataMaxBytes {
			var removed int
			if err := tx.QueryRowContext(ctx, `DELETE FROM metadata WHERE (indexer_id, identity) =
				(SELECT indexer_id, identity FROM metadata ORDER BY used_ns, indexer_id, identity LIMIT 1)
				RETURNING length(data)`).Scan(&removed); err != nil {
				return err
			}
			count--
			size -= removed
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO metadata VALUES (?, ?, ?, ?, ?)", key.indexerID, key.identity[:], data,
			now.Add(metadataTTL).UnixNano(), now.UnixNano())
		return err
	})
}
