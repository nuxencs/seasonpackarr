// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// The external test package limits the source tests to Begin and the run methods.
package discovery_test

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/discovery"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger/loggertest"
	"github.com/nuxencs/seasonpackarr/internal/state"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// defaultIndexers holds, in priority order for a search: 2 (search only),
// 1 (search and RSS); for RSS: 3 (RSS only), 1. Indexer 4 is disabled and
// indexer 5 is a Usenet indexer, so Prowlarr gives neither to a run. Indexer 1
// has pages of 2 listings. Indexer 2 has no pagination.
const defaultIndexers = `[
	{"id":1,"priority":2,"enable":true,"protocol":"torrent","supportsSearch":true,"supportsRss":true,"supportsPagination":true,"capabilities":{"searchParams":["q"],"limitsMax":2}},
	{"id":2,"priority":1,"enable":true,"protocol":"torrent","supportsSearch":true,"capabilities":{"searchParams":["q"]}},
	{"id":3,"priority":1,"enable":true,"protocol":"torrent","supportsRss":true},
	{"id":4,"priority":1,"enable":false,"protocol":"torrent","supportsSearch":true,"supportsRss":true},
	{"id":5,"priority":1,"enable":true,"protocol":"usenet","supportsSearch":true,"supportsRss":true}
]`

// showQuery is the query of the search tests. Its failure text is "Show S01".
var showQuery = discovery.Query{Title: "Show", Season: 1}

// capturedRequest is one request that the fake Prowlarr received.
type capturedRequest struct {
	Path  string
	Query url.Values
	At    time.Time
}

// fakeProwlarr answers the requests of the source in process.
type fakeProwlarr struct {
	apiKey   string
	indexers string
	// listings holds the titles of the feed of each indexer, by indexer id.
	// The feed gives the page that the offset and the limit select.
	listings map[int][]string
	// torrents holds the bytes of a download by title. A title that is not
	// in the map gets a valid multi-file torrent.
	torrents map[string][]byte
	// respond, when set, answers a request in place of the default answer.
	respond  func(*http.Request) *http.Response
	requests []capturedRequest
}

func (f *fakeProwlarr) RoundTrip(r *http.Request) (*http.Response, error) {
	f.requests = append(f.requests, capturedRequest{Path: r.URL.Path, Query: r.URL.Query(), At: time.Now()})
	if r.Header.Get("X-Api-Key") != f.apiKey {
		return response(http.StatusUnauthorized, ""), nil
	}
	if f.respond != nil {
		if resp := f.respond(r); resp != nil {
			return resp, nil
		}
	}
	if r.URL.Path == "/api/v1/indexer" {
		return response(http.StatusOK, f.indexers), nil
	}
	if rest, ok := strings.CutSuffix(r.URL.Path, "/api"); ok {
		id, err := strconv.Atoi(strings.TrimPrefix(rest, "/"))
		if err != nil {
			return response(http.StatusNotFound, ""), nil
		}
		titles := f.listings[id]
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		titles = titles[min(offset, len(titles)):min(offset+limit, len(titles))]
		return feedResponse(id, titles...), nil
	}
	if strings.HasSuffix(r.URL.Path, "/download") {
		title := r.URL.Query().Get("title")
		if data, ok := f.torrents[title]; ok {
			return response(http.StatusOK, string(data)), nil
		}
		return response(http.StatusOK, string(multiFileTorrent(title))), nil
	}
	return response(http.StatusNotFound, ""), nil
}

// feedResponse is a Torznab feed of indexer id with one listing for each
// title. The identity of a listing comes from its title, so a repeated title
// is a repeated listing.
func feedResponse(id int, titles ...string) *http.Response {
	type item struct {
		Title string `xml:"title"`
		GUID  string `xml:"guid"`
		Link  string `xml:"link"`
	}
	feed := struct {
		XMLName xml.Name `xml:"rss"`
		Items   []item   `xml:"channel>item"`
	}{}
	for _, title := range titles {
		feed.Items = append(feed.Items, item{
			Title: title,
			GUID:  fmt.Sprintf("%d-%s", id, title),
			Link:  fmt.Sprintf("http://prowlarr.invalid/%d/download?title=%s", id, url.QueryEscape(title)),
		})
	}
	data, err := xml.Marshal(feed)
	if err != nil {
		panic(err)
	}
	return response(http.StatusOK, string(data))
}

func multiFileTorrent(name string) []byte {
	return torrentBytes(metainfo.Info{Name: name, PieceLength: 16384, Files: []metainfo.FileInfo{{Length: 1, Path: []string{name + ".E01.mkv"}}}})
}

func torrentBytes(info metainfo.Info) []byte {
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		panic(err)
	}
	var torrent bytes.Buffer
	if err := (&metainfo.MetaInfo{InfoBytes: infoBytes}).Write(&torrent); err != nil {
		panic(err)
	}
	return torrent.Bytes()
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

// cooldownResponse is a temporary failure with a Retry-After deadline in seconds.
func cooldownResponse(status int, retryAfter string) *http.Response {
	resp := response(status, "")
	resp.Header.Set("Retry-After", retryAfter)
	return resp
}

// sourceFixture is a source on a real SQLite file and a fake Prowlarr. It
// needs no torrent client, no pack, and no config provider.
type sourceFixture struct {
	path     string
	store    *state.Store
	source   *discovery.Source
	prowlarr *fakeProwlarr
	log      *loggertest.Logger
	settings domain.Search
}

func newSourceFixture(t *testing.T) *sourceFixture {
	t.Helper()
	f := &sourceFixture{
		path:     filepath.Join(t.TempDir(), "seasonpackarr.db"),
		prowlarr: &fakeProwlarr{apiKey: "prowlarr-test-key", indexers: defaultIndexers},
		log:      loggertest.New(),
		settings: domain.Search{ProwlarrURL: "http://prowlarr.invalid", APIKey: "prowlarr-test-key", RequestInterval: "0s"},
	}
	f.open(t)
	return f
}

func (f *sourceFixture) open(t *testing.T) {
	t.Helper()
	store, err := state.Open(t.Context(), f.path, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	f.store = store
	f.source = discovery.New(store, f.log, f.prowlarr)
}

// restart closes the store, opens it again, and makes a new source, so only
// what SQLite keeps survives.
func (f *sourceFixture) restart(t *testing.T) {
	t.Helper()
	require.NoError(t, f.store.Close())
	f.open(t)
}

// begin opens a run of kind and ends it at the end of the test.
func (f *sourceFixture) begin(t *testing.T, kind discovery.Kind) *discovery.Run {
	t.Helper()
	run, err := f.source.Begin(t.Context(), f.settings, kind)
	require.NoError(t, err)
	t.Cleanup(run.End)
	return run
}

// indexers runs one Indexers call in a run of kind, ends the run, and returns
// the usable indexer ids and the failures.
func (f *sourceFixture) indexers(t *testing.T, kind discovery.Kind) ([]int, []discovery.Failure) {
	t.Helper()
	run, err := f.source.Begin(t.Context(), f.settings, kind)
	require.NoError(t, err)
	defer run.End()
	var ids []int
	for _, indexer := range run.Indexers(t.Context()) {
		ids = append(ids, indexer.ID)
	}
	return ids, run.Failures()
}

// requestCount is the number of requests that the fake Prowlarr received.
func (f *sourceFixture) requestCount() int { return len(f.prowlarr.requests) }

// requestsTo returns the requests that the fake Prowlarr received for path.
func (f *sourceFixture) requestsTo(path string) []capturedRequest {
	var requests []capturedRequest
	for _, request := range f.prowlarr.requests {
		if request.Path == path {
			requests = append(requests, request)
		}
	}
	return requests
}

// usableIndexers calls Indexers on run and returns the usable indexers by id.
func usableIndexers(t *testing.T, run *discovery.Run) map[int]discovery.Indexer {
	t.Helper()
	indexers := make(map[int]discovery.Indexer)
	for _, indexer := range run.Indexers(t.Context()) {
		indexers[indexer.ID] = indexer
	}
	return indexers
}

// episodeTitles returns n episode titles of showQuery.
func episodeTitles(n int) []string {
	titles := make([]string, n)
	for i := range titles {
		titles[i] = fmt.Sprintf("Show.S01E%02d", i+1)
	}
	return titles
}

// accept is a relevance predicate of a feed that accepts only the given titles.
func accept(titles ...string) func(string) bool {
	return func(title string) bool { return slices.Contains(titles, title) }
}

// titles collects the titles of all listings of a sequence.
func titles(listings iter.Seq[*discovery.Listing]) []string {
	var titles []string
	for listing := range listings {
		titles = append(titles, listing.Title())
	}
	return titles
}

// firstListing returns the first listing of a search for showQuery on indexer 2.
func firstListing(t *testing.T, run *discovery.Run) *discovery.Listing {
	t.Helper()
	for listing := range run.Search(t.Context(), usableIndexers(t, run)[2], showQuery) {
		return listing
	}
	require.FailNow(t, "the search gave no listing")
	return nil
}

// fetchFirst runs one search run that gets the torrent of the first listing
// on indexer 2. It returns the bytes and the counts of the run.
func (f *sourceFixture) fetchFirst(t *testing.T) ([]byte, discovery.Stats) {
	t.Helper()
	run, err := f.source.Begin(t.Context(), f.settings, discovery.KindSearch)
	require.NoError(t, err)
	defer run.End()
	data, err := firstListing(t, run).Torrent(t.Context())
	require.NoError(t, err)
	require.Empty(t, run.Failures())
	return data, run.Stats()
}

// searchAll searches showQuery on the indexer, gets the torrent of each
// listing, and returns the titles. A failed download does not stop it.
func searchAll(t *testing.T, run *discovery.Run, indexer discovery.Indexer) []string {
	t.Helper()
	var got []string
	for listing := range run.Search(t.Context(), indexer, showQuery) {
		got = append(got, listing.Title())
		_, _ = listing.Torrent(t.Context())
	}
	return got
}

// at answers the requests to path with a new response from respond.
func at(path string, respond func() *http.Response) func(*http.Request) *http.Response {
	return func(r *http.Request) *http.Response {
		if r.URL.Path != path {
			return nil
		}
		return respond()
	}
}

// atTitle answers the download of title with a new response from respond.
func atTitle(title string, respond func() *http.Response) func(*http.Request) *http.Response {
	return func(r *http.Request) *http.Response {
		if !strings.HasSuffix(r.URL.Path, "/download") || r.URL.Query().Get("title") != title {
			return nil
		}
		return respond()
	}
}
