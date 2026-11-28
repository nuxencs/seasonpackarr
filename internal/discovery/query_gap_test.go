// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package discovery_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/discovery"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// slowAnswer is the time that the fake Prowlarr takes to answer a feed page in
// the query gap tests. It shows that the gap starts at the answer.
const slowAnswer = 5 * time.Second

// answerSlowly makes the fake Prowlarr take slowAnswer for each feed page.
func answerSlowly(r *http.Request) *http.Response {
	if strings.HasSuffix(r.URL.Path, "/api") {
		time.Sleep(slowAnswer)
	}
	return nil
}

// gapSinceAnswer is the time from the answer of a feed page to the start of
// the next one.
func gapSinceAnswer(pages []capturedRequest, i int) time.Duration {
	return pages[i].At.Sub(pages[i-1].At) - slowAnswer
}

func TestQueryGap_SearchPages(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSourceFixture(t)
		f.settings.RequestInterval = "10s"
		f.prowlarr.listings = map[int][]string{1: episodeTitles(5)}
		f.prowlarr.respond = answerSlowly
		run := f.begin(t, discovery.KindSearch)

		titles(run.Search(t.Context(), usableIndexers(t, run)[1], showQuery))

		pages := f.requestsTo("/1/api")
		require.Len(t, pages, 3)
		assert.Equal(t, time.Minute, gapSinceAnswer(pages, 1))
		assert.Equal(t, time.Minute, gapSinceAnswer(pages, 2))
		assert.Empty(t, run.Failures())
	})
}

func TestQueryGap_FeedPages(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSourceFixture(t)
		f.prowlarr.listings = map[int][]string{1: {"Old.S01E01"}}
		f.poll(t, 1, accept())
		time.Sleep(time.Hour)
		f.prowlarr.listings[1] = []string{"Show.S01E03", "Show.S01E02", "Show.S01E01", "Old.S01E01"}
		f.prowlarr.respond = answerSlowly
		f.prowlarr.requests = nil

		_, failures := f.poll(t, 1, accept())

		pages := f.requestsTo("/1/api")
		require.Len(t, pages, 2, "the poll must read back to the checkpoint")
		assert.Equal(t, time.Minute, gapSinceAnswer(pages, 1))
		assert.Empty(t, failures)
	})
}

func TestQueryGap_HoldsAcrossRuns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSourceFixture(t)
		f.prowlarr.listings = map[int][]string{1: {"Show.S01"}}
		f.prowlarr.respond = answerSlowly
		f.poll(t, 1, accept())
		time.Sleep(20 * time.Second)

		run := f.begin(t, discovery.KindSearch)
		titles(run.Search(t.Context(), usableIndexers(t, run)[1], showQuery))

		pages := f.requestsTo("/1/api")
		require.Len(t, pages, 2)
		assert.Equal(t, time.Minute, gapSinceAnswer(pages, 1), "a search after a poll must wait for the rest of the gap")
		assert.Empty(t, run.Failures())
	})
}

func TestQueryGap_OtherIndexersUseRequestInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSourceFixture(t)
		f.settings.RequestInterval = "10s"
		f.prowlarr.listings = map[int][]string{1: {"Show.S01"}, 2: {"Show.S01"}}
		run := f.begin(t, discovery.KindSearch)
		indexers := usableIndexers(t, run)

		titles(run.Search(t.Context(), indexers[2], showQuery))
		titles(run.Search(t.Context(), indexers[1], showQuery))

		first, second := f.requestsTo("/2/api"), f.requestsTo("/1/api")
		require.Len(t, first, 1)
		require.Len(t, second, 1)
		assert.Equal(t, 10*time.Second, second[0].At.Sub(first[0].At))
		assert.Empty(t, run.Failures())
	})
}

func TestQueryGap_DownloadDoesNotWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSourceFixture(t)
		f.settings.RequestInterval = "10s"
		f.prowlarr.listings = map[int][]string{2: {"Show.S01", "Show.S01.REPACK"}}
		run := f.begin(t, discovery.KindSearch)

		searchAll(t, run, usableIndexers(t, run)[2])

		page, downloads := f.requestsTo("/2/api"), f.requestsTo("/2/download")
		require.Len(t, page, 1)
		require.Len(t, downloads, 2)
		assert.Equal(t, 10*time.Second, downloads[0].At.Sub(page[0].At))
		assert.Equal(t, 10*time.Second, downloads[1].At.Sub(downloads[0].At))
		assert.Empty(t, run.Failures())
	})
}

func TestQueryGap_CanceledWait(t *testing.T) {
	for name, tt := range map[string]struct {
		kind discovery.Kind
		read func(context.Context, *discovery.Run, discovery.Indexer) []string
	}{
		"search": {discovery.KindSearch, func(ctx context.Context, run *discovery.Run, indexer discovery.Indexer) []string {
			return titles(run.Search(ctx, indexer, showQuery))
		}},
		"feed": {discovery.KindRSS, func(ctx context.Context, run *discovery.Run, indexer discovery.Indexer) []string {
			return titles(run.Feed(ctx, indexer, accept("Show.S01")))
		}},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newSourceFixture(t)
				f.prowlarr.listings = map[int][]string{1: {"Show.S01"}}
				f.poll(t, 1, accept())
				run := f.begin(t, tt.kind)
				indexer := usableIndexers(t, run)[1]
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
				defer cancel()
				start := time.Now()

				assert.Empty(t, tt.read(ctx, run, indexer))
				run.End()

				assert.Equal(t, 20*time.Second, time.Since(start), "a canceled run must end the wait at once")
				assert.Len(t, f.requestsTo("/1/api"), 1, "a canceled wait must send no query")
				assert.Zero(t, run.Stats().FeedPages, "a canceled wait must count no feed page")
				assert.Empty(t, run.Failures(), "the caller reports a canceled context")
				assert.NoError(t, run.Err())
				ids, failures := f.indexers(t, tt.kind)
				assert.Contains(t, ids, 1, "a canceled wait must create no cooldown")
				assert.Empty(t, failures)
			})
		})
	}
}
