// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metadata

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/autobrr/rls"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

type fakeProvider struct {
	calls    atomic.Int32
	episodes int
	err      error
	delay    time.Duration
}

func (f *fakeProvider) episodesInSeason(release rls.Release) (int, error) {
	f.calls.Add(1)
	time.Sleep(f.delay)
	return f.episodes, f.err
}

func newTestProvider(fake *fakeProvider) *Provider {
	return &Provider{
		log:          zerolog.Nop(),
		tvmazeClient: fake,
		cache:        make(map[string]cacheEntry),
	}
}

func Test_EpisodesInSeason_CachesWithinTTL(t *testing.T) {
	fake := &fakeProvider{episodes: 10}
	p := newTestProvider(fake)
	release := rls.Release{Title: "Some Show", Series: 1}

	for range 3 {
		got, err := p.EpisodesInSeason(release)
		assert.NoError(t, err)
		assert.Equal(t, 10, got)
	}

	assert.Equal(t, int32(1), fake.calls.Load())
}

func Test_EpisodesInSeason_RefetchesAfterExpiry(t *testing.T) {
	fake := &fakeProvider{episodes: 10}
	p := newTestProvider(fake)
	release := rls.Release{Title: "Some Show", Series: 1}

	_, err := p.EpisodesInSeason(release)
	assert.NoError(t, err)

	// expire the cached entry
	p.cacheMu.Lock()
	for key, entry := range p.cache {
		entry.expiresAt = time.Now().Add(-time.Minute)
		p.cache[key] = entry
	}
	p.cacheMu.Unlock()

	_, err = p.EpisodesInSeason(release)
	assert.NoError(t, err)
	assert.Equal(t, int32(2), fake.calls.Load())
}

func Test_EpisodesInSeason_CollapsesConcurrentLookups(t *testing.T) {
	fake := &fakeProvider{episodes: 10, delay: 50 * time.Millisecond}
	p := newTestProvider(fake)
	release := rls.Release{Title: "Some Show", Series: 1}

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			got, err := p.EpisodesInSeason(release)
			assert.NoError(t, err)
			assert.Equal(t, 10, got)
		})
	}
	wg.Wait()

	assert.Equal(t, int32(1), fake.calls.Load())
}

func Test_EpisodesInSeason_DoesNotCacheErrors(t *testing.T) {
	fake := &fakeProvider{err: fmt.Errorf("provider down")}
	p := newTestProvider(fake)
	release := rls.Release{Title: "Some Show", Series: 1}

	for range 2 {
		_, err := p.EpisodesInSeason(release)
		assert.Error(t, err)
	}

	assert.Equal(t, int32(2), fake.calls.Load())
}

func Test_EpisodesInSeason_EvictsExpiredEntriesOnStore(t *testing.T) {
	fake := &fakeProvider{episodes: 10}
	p := newTestProvider(fake)

	_, err := p.EpisodesInSeason(rls.Release{Title: "Old Show", Series: 1})
	assert.NoError(t, err)
	_, err = p.EpisodesInSeason(rls.Release{Title: "Stale Show", Series: 1})
	assert.NoError(t, err)

	// expire "Old Show" and "Stale Show"
	p.cacheMu.Lock()
	for key, entry := range p.cache {
		entry.expiresAt = time.Now().Add(-time.Minute)
		p.cache[key] = entry
	}
	p.cacheMu.Unlock()

	// storing a fresh entry sweeps the expired ones
	_, err = p.EpisodesInSeason(rls.Release{Title: "New Show", Series: 1})
	assert.NoError(t, err)

	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()
	assert.Len(t, p.cache, 1)
}

func Test_EpisodesInSeason_SeparateCacheKeys(t *testing.T) {
	fake := &fakeProvider{episodes: 10}
	p := newTestProvider(fake)

	_, err := p.EpisodesInSeason(rls.Release{Title: "Some Show", Series: 1})
	assert.NoError(t, err)
	_, err = p.EpisodesInSeason(rls.Release{Title: "Some Show", Series: 2})
	assert.NoError(t, err)
	_, err = p.EpisodesInSeason(rls.Release{Title: "Other Show", Series: 1})
	assert.NoError(t, err)

	assert.Equal(t, int32(3), fake.calls.Load())
}
