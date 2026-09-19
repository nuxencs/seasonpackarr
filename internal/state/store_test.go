// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package state

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/prowlarr"
	"github.com/stretchr/testify/require"
)

func testStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state %#", "seasonpackarr.db")
	s, err := Open(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	return s, path
}

func TestStore_RestartAndConnectionIsolation(t *testing.T) {
	for _, change := range []string{"none", "url", "key"} {
		t.Run(change, func(t *testing.T) {
			s, path := testStore(t)
			ctx := t.Context()
			now := time.Now()
			require.NoError(t, s.UseConnection(ctx, "http://prowlarr", "secret"))
			result := prowlarr.Result{Title: "pack", GUID: "one", Link: "/download"}
			key := MetadataKey(1, result)
			require.NoError(t, s.PutMetadata(ctx, key, []byte("torrent"), now))
			require.NoError(t, s.SetCooldown(ctx, 0, now.Add(time.Hour)))
			require.NoError(t, s.SetCooldown(ctx, 1, now.Add(2*time.Hour)))
			require.NoError(t, s.SetCooldown(ctx, 1, now.Add(time.Hour)))
			feeds, err := s.Feeds(ctx)
			require.NoError(t, err)
			feeds.Remember(key, result, now)
			feeds.SetCheckpoint(1, map[Key]bool{key: true})
			require.NoError(t, s.SaveFeeds(ctx, feeds))
			require.NoError(t, s.Close())
			s, err = Open(ctx, path)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, s.Close()) })
			address, secret := "http://prowlarr", "secret"
			if change == "url" {
				address += "/"
			}
			if change == "key" {
				secret += "-changed"
			}
			require.NoError(t, s.UseConnection(ctx, address, secret))
			metadata, err := s.Metadata(ctx, key, now.Add(time.Minute))
			require.NoError(t, err)
			deadlines, err := s.Cooldowns(ctx, now)
			require.NoError(t, err)
			loaded, err := s.Feeds(ctx)
			require.NoError(t, err)
			if change != "none" {
				require.Empty(t, metadata)
				require.Empty(t, deadlines)
				require.Empty(t, loaded.entries)
				require.Empty(t, loaded.checkpoints)
				return
			}
			require.Equal(t, []byte("torrent"), metadata)
			require.True(t, deadlines[0].Equal(now.Add(time.Hour)))
			require.True(t, deadlines[1].Equal(now.Add(2*time.Hour)))
			require.Equal(t, feeds.Results(1), loaded.Results(1))
			require.Equal(t, feeds.Checkpoint(1), loaded.Checkpoint(1))
			require.True(t, loaded.entries[key].expires.Equal(now.Add(rssRetention)))
			deadlines, err = s.Cooldowns(ctx, now.Add(2*time.Hour))
			require.NoError(t, err)
			require.Empty(t, deadlines)
		})
	}
}

func TestStore_PrivateFileAndSchema(t *testing.T) {
	s, path := testStore(t)
	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	var version int
	require.NoError(t, s.db.QueryRow("PRAGMA user_version").Scan(&version))
	require.Equal(t, 1, version)
	_, err = s.db.Exec("PRAGMA user_version = 99")
	require.NoError(t, err)
	require.NoError(t, s.Close())
	_, err = Open(t.Context(), path)
	require.ErrorContains(t, err, "unsupported database schema version 99")
}

func TestStore_RejectsInvalidFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.db")
	content := []byte("not a database")
	require.NoError(t, os.WriteFile(path, content, 0o600))
	_, err := Open(t.Context(), path)
	require.Error(t, err)
	actual, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, content, actual)
	_, err = Open(t.Context(), t.TempDir())
	require.ErrorContains(t, err, "regular file")
	if runtime.GOOS != "windows" {
		link := filepath.Join(t.TempDir(), "state.db")
		require.NoError(t, os.Symlink(path, link))
		_, err = Open(t.Context(), link)
		require.ErrorContains(t, err, "regular file")
	}
}

func TestStore_StartupPrunesExpiredState(t *testing.T) {
	s, path := testStore(t)
	ctx := t.Context()
	now := time.Now().Add(-8 * 24 * time.Hour)
	result := prowlarr.Result{Title: "expired", GUID: "expired"}
	key := MetadataKey(1, result)
	require.NoError(t, s.PutMetadata(ctx, key, []byte("torrent"), now))
	require.NoError(t, s.SetCooldown(ctx, 1, now.Add(time.Hour)))
	feeds, err := s.Feeds(ctx)
	require.NoError(t, err)
	feeds.Remember(key, result, now)
	feeds.SetCheckpoint(1, map[Key]bool{key: true})
	require.NoError(t, s.SaveFeeds(ctx, feeds))
	require.NoError(t, s.Close())
	s, err = Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	for _, table := range []string{"metadata", "cooldowns", "rss_candidates"} {
		var count int
		require.NoError(t, s.db.QueryRow("SELECT count(*) FROM "+table).Scan(&count))
		require.Zero(t, count, table)
	}
	loaded, err := s.Feeds(ctx)
	require.NoError(t, err)
	require.Equal(t, map[Key]bool{key: true}, loaded.Checkpoint(1), "keep checkpoints so long gaps remain detectable")
}

func TestPath(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	path, err := Path("config")
	require.NoError(t, err)
	require.Equal(t, filepath.Join("config", "seasonpackarr.db"), path)
	path, err = Path("")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(os.Getenv("XDG_DATA_HOME"), "seasonpackarr", "seasonpackarr.db"), path)
	t.Setenv("XDG_DATA_HOME", "")
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	path, err = Path("")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".local", "share", "seasonpackarr", "seasonpackarr.db"), path)
}
