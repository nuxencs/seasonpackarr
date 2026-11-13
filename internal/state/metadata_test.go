// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package state

import (
	"fmt"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/prowlarr"
	"github.com/stretchr/testify/require"
)

func TestMetadata_ExpiryIdentityAndEviction(t *testing.T) {
	s, _ := testStore(t)
	ctx := t.Context()
	now := time.Now()
	key := MetadataKey(1, prowlarr.Result{GUID: "pack", Title: "show"})
	get := func(key Key, at time.Time) []byte {
		data, err := s.Metadata(ctx, key, at)
		require.NoError(t, err)
		return data
	}
	require.NoError(t, s.PutMetadata(ctx, key, []byte("torrent"), now))
	require.Equal(t, []byte("torrent"), get(key, now.Add(time.Hour)))
	require.Nil(t, get(MetadataKey(2, prowlarr.Result{GUID: "pack", Title: "show"}), now))
	require.Nil(t, get(MetadataKey(1, prowlarr.Result{GUID: "pack", Title: "other"}), now))
	require.Nil(t, get(key, now.Add(metadataTTL)))
	require.NoError(t, s.PutMetadata(ctx, MetadataKey(1, prowlarr.Result{Title: "show"}), []byte("no identity"), now))
	var count int
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM metadata").Scan(&count))
	require.Zero(t, count)
	for i := range metadataMaxEntries {
		require.NoError(t, s.PutMetadata(ctx, MetadataKey(1, prowlarr.Result{GUID: fmt.Sprint(i)}), []byte{1}, now.Add(time.Duration(i))))
	}
	require.NotNil(t, get(MetadataKey(1, prowlarr.Result{GUID: "0"}), now.Add(time.Second)))
	require.NoError(t, s.PutMetadata(ctx, key, []byte("torrent"), now.Add(2*time.Second)))
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM metadata").Scan(&count))
	require.Equal(t, metadataMaxEntries, count)
	require.Nil(t, get(MetadataKey(1, prowlarr.Result{GUID: "1"}), now.Add(3*time.Second)))
	require.NotNil(t, get(MetadataKey(1, prowlarr.Result{GUID: "0"}), now.Add(3*time.Second)))
}

func TestMetadata_ByteBudget(t *testing.T) {
	s, _ := testStore(t)
	ctx := t.Context()
	now := time.Now()
	first, second := MetadataKey(1, prowlarr.Result{GUID: "first"}), MetadataKey(1, prowlarr.Result{GUID: "second"})
	require.NoError(t, s.PutMetadata(ctx, first, make([]byte, metadataMaxBytes), now))
	require.NoError(t, s.PutMetadata(ctx, second, []byte{1}, now.Add(time.Second)))
	data, err := s.Metadata(ctx, first, now.Add(2*time.Second))
	require.NoError(t, err)
	require.Nil(t, data)
	data, err = s.Metadata(ctx, second, now.Add(2*time.Second))
	require.NoError(t, err)
	require.Equal(t, []byte{1}, data)
	var size int
	require.NoError(t, s.db.QueryRow("SELECT sum(length(data)) FROM metadata").Scan(&size))
	require.Equal(t, 1, size)
}
