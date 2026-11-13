// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"cmp"
	"crypto/sha256"
	"encoding/json"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/prowlarr"
)

const (
	searchMetadataTTL        = 7 * 24 * time.Hour
	searchMetadataMaxBytes   = 64 << 20
	searchMetadataMaxEntries = 1024
)

type searchMetadataKey struct {
	indexerID int
	identity  [sha256.Size]byte
}

type searchMetadataEntry struct {
	data    []byte
	expires time.Time
	used    time.Time
}

// Access is serialized by searchRunner.running. Only metadata is cached, never
// inventory, source availability, coverage, or import decisions.
type searchMetadataCache struct {
	entries map[searchMetadataKey]searchMetadataEntry
	bytes   int
}

func metadataKey(indexerID int, result prowlarr.Result) searchMetadataKey {
	identity := cmp.Or(result.GUID, result.Link, result.Enclosure.URL)
	if identity == "" {
		return searchMetadataKey{}
	}
	// Fixed-size keys keep large result titles or links outside the cache budget.
	encoded, _ := json.Marshal([2]string{identity, result.Title})
	return searchMetadataKey{indexerID, sha256.Sum256(encoded)}
}

func (c *searchMetadataCache) get(key searchMetadataKey, now time.Time) []byte {
	entry, ok := c.entries[key]
	if !ok {
		return nil
	}
	if !now.Before(entry.expires) {
		c.remove(key)
		return nil
	}
	entry.used = now
	c.entries[key] = entry
	return entry.data
}

func (c *searchMetadataCache) put(key searchMetadataKey, data []byte, now time.Time) {
	if key.indexerID <= 0 || len(data) == 0 || len(data) > searchMetadataMaxBytes {
		return
	}
	if c.entries == nil {
		c.entries = make(map[searchMetadataKey]searchMetadataEntry)
	}
	c.remove(key)
	for k, entry := range c.entries {
		if !now.Before(entry.expires) {
			c.remove(k)
		}
	}
	for c.bytes+len(data) > searchMetadataMaxBytes || len(c.entries) >= searchMetadataMaxEntries {
		var oldest searchMetadataKey
		var used time.Time
		for k, entry := range c.entries {
			if used.IsZero() || entry.used.Before(used) {
				oldest, used = k, entry.used
			}
		}
		c.remove(oldest)
	}
	c.entries[key] = searchMetadataEntry{data: data, expires: now.Add(searchMetadataTTL), used: now}
	c.bytes += len(data)
}

func (c *searchMetadataCache) remove(key searchMetadataKey) {
	c.bytes -= len(c.entries[key].data)
	delete(c.entries, key)
}
