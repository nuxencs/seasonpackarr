// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package state owns the SQLite schema and bounded discovery state.
package state

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"
	_ "modernc.org/sqlite"
)

//go:embed migrations/001_discovery.sql
var initialSchema string

// Store is owned by the application lifecycle. Discovery operations are serialized
// by the discovery runner; the store is not a multi-process scheduler.
type Store struct {
	db *sql.DB
}

// Path keeps state beside the active config, or in the user data directory for
// environment-only installations without an explicit --config directory.
func Path(configDir string) (string, error) {
	if configDir == "" {
		base := os.Getenv("XDG_DATA_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("locate application data directory: %w", err)
			}
			base = filepath.Join(home, ".local", "share")
		}
		configDir = filepath.Join(base, "seasonpackarr")
	}
	return filepath.Join(configDir, "seasonpackarr.db"), nil
}

// Open creates or migrates a private database. An unreadable, corrupt, or newer
// schema is an error, never a reason to erase state or use volatile storage.
func Open(ctx context.Context, path string, log zerolog.Logger) (*Store, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	log = log.With().Str("module", "database").Logger()
	log.Info().Str("path", path).Msg("opening discovery database")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		err = file.Close()
	} else if errors.Is(err, os.ErrExist) {
		var info os.FileInfo
		info, err = os.Lstat(path)
		if err == nil && !info.Mode().IsRegular() {
			err = errors.New("database must be a regular file, not a symlink or directory")
		}
	}
	if err != nil {
		return nil, fmt.Errorf("open database file: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("protect database file: %w", err)
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	// File URIs need a leading slash before a Windows drive letter.
	if !strings.HasPrefix(uri.Path, "/") {
		uri.Path = "/" + uri.Path
	}
	query := url.Values{}
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "journal_mode(WAL)")
	// Synchronize each committed WAL transaction, not only periodic checkpoints.
	query.Add("_pragma", "synchronous(FULL)")
	uri.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	// One connection keeps transactions and PRAGMA behavior predictable.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	version, err := s.migrate(ctx, log)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize discovery database: %w", err)
	}
	if err := s.pruneExpired(ctx, time.Now()); err != nil {
		db.Close()
		return nil, fmt.Errorf("prune expired discovery state: %w", err)
	}
	var journalMode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		db.Close()
		return nil, fmt.Errorf("read database journal mode: %w", err)
	}
	log.Info().Int("schema_version", version).Str("journal_mode", journalMode).Msg("discovery database ready")
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) transaction(ctx context.Context, apply func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := apply(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) migrate(ctx context.Context, log zerolog.Logger) (int, error) {
	migrations := []string{initialSchema}
	var version int
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
			return err
		}
		if version < 0 || version > len(migrations) {
			return fmt.Errorf("unsupported database schema version %d; use a compatible seasonpackarr version", version)
		}
		log = log.With().Int("from_version", version).Int("to_version", len(migrations)).Int("count", len(migrations)-version).Logger()
		if version < len(migrations) {
			log.Info().Msg("applying database migrations")
		}
		for i := version; i < len(migrations); i++ {
			if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
				return fmt.Errorf("apply schema version %d: %w", i+1, err)
			}
			if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
				return fmt.Errorf("record schema version %d: %w", i+1, err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	// Report success only after the migration transaction commits.
	if version < len(migrations) {
		log.Info().Msg("database migrations applied")
	}
	return len(migrations), nil
}

// UseConnection invalidates all discovery state when the URL or credential
// changes. Only a fingerprint is stored, never the connection API key.
func (s *Store) UseConnection(ctx context.Context, address, apiKey string) error {
	if err := s.pruneExpired(ctx, time.Now()); err != nil {
		return err
	}
	mac := hmac.New(sha256.New, []byte(apiKey))
	mac.Write([]byte(address))
	fingerprint := mac.Sum(nil)
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var previous []byte
		err := tx.QueryRowContext(ctx, "SELECT fingerprint FROM connection WHERE id = 1").Scan(&previous)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if hmac.Equal(previous, fingerprint) {
			return nil
		}
		for _, table := range []string{"metadata", "cooldowns", "rss_candidates", "rss_checkpoints"} {
			if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO connection VALUES (1, ?) ON CONFLICT(id) DO UPDATE SET fingerprint = excluded.fingerprint", fingerprint)
		return err
	})
}

func (s *Store) pruneExpired(ctx context.Context, now time.Time) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		for _, table := range []string{"metadata", "rss_candidates"} {
			if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE expires_ns <= ?", now.UnixNano()); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM cooldowns WHERE until_ns <= ?", now.UnixNano())
		return err
	})
}

func (s *Store) Cooldowns(ctx context.Context, now time.Time) (map[int]time.Time, error) {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM cooldowns WHERE until_ns <= ?", now.UnixNano()); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT indexer_id, until_ns FROM cooldowns")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	deadlines := make(map[int]time.Time)
	for rows.Next() {
		var id int
		var until int64
		if err := rows.Scan(&id, &until); err != nil {
			return nil, err
		}
		deadlines[id] = time.Unix(0, until)
	}
	return deadlines, rows.Err()
}

func (s *Store) SetCooldown(ctx context.Context, indexerID int, until time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO cooldowns VALUES (?, ?)
		ON CONFLICT(indexer_id) DO UPDATE SET until_ns = max(until_ns, excluded.until_ns)`, indexerID, until.UnixNano())
	return err
}
