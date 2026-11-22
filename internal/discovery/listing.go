// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package discovery

import (
	"context"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/prowlarr"
	"github.com/nuxencs/seasonpackarr/internal/state"
	"github.com/nuxencs/seasonpackarr/internal/torrents"
)

// Listing is one release that an indexer offers. It hides its identity and
// its proxy URL, which can hold a tracker passkey.
type Listing struct {
	run       *Run
	indexerID int
	result    prowlarr.Result

	// fetched is true after the first download, including a failed download.
	// The listing keeps the bytes and the error of it.
	fetched bool
	data    []byte
	err     error
}

// Title is the release name of the listing.
func (l *Listing) Title() string { return l.result.Title }

// IndexerID is the id of the indexer that offers the listing.
func (l *Listing) IndexerID() int { return l.indexerID }

// Torrent returns the torrent bytes of the listing. It reads the torrent cache,
// else it downloads the torrent through Prowlarr. It downloads one time for
// the listing and keeps the result, also a failure. Only bytes that parse as a
// torrent go into the torrent cache, but other bytes still go to the caller.
//
// A failed download matches ErrDownload and has the text of the failure. A
// cooldown error of the download takes the indexer out. After a database
// failure of the run, the error matches ErrDatabase. A canceled context
// returns its error.
func (l *Listing) Torrent(ctx context.Context) ([]byte, error) {
	r := l.run
	if r.err != nil {
		return nil, r.err
	}
	if l.fetched {
		return l.data, l.err
	}
	key := state.MetadataKey(l.indexerID, l.result)
	data, err := r.source.store.Metadata(ctx, key, time.Now())
	if err != nil {
		r.databaseFailure(err)
		return nil, r.err
	}
	if data != nil {
		r.stats.CacheHits++
		l.fetched, l.data = true, data
		return data, nil
	}
	data, err = r.source.client.Download(ctx, l.indexerID, l.result)
	l.fetched = true
	if err != nil {
		l.err = err
		if !canceled(err) {
			l.err = classError{ErrDownload, err}
			// Rule 4 applies to a download that is not a cooldown: the indexer continues.
			r.recordCooldown(ctx, l.indexerID, err)
		}
		return nil, l.err
	}
	r.stats.Downloads++
	l.data = data
	if _, err := torrents.ParseInfo(data); err == nil {
		if err := r.source.store.PutMetadata(ctx, key, data, time.Now()); err != nil {
			r.databaseFailure(err)
			return nil, r.err
		}
	}
	return data, nil
}
