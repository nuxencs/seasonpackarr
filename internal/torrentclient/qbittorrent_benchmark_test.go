// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/autobrr/go-qbittorrent"
)

// benchmarkInventoryCategories copies the client from issue #326: 5341
// torrents behind qui, about 2 KB of JSON each.
var benchmarkInventoryCategories = []struct {
	name  string
	count int
	tags  string
}{
	{name: "tv-hd-completed", count: 902, tags: "tv"},
	{name: "tv-hd-completed.cross-seed", count: 302, tags: "tv, cross-seed"},
	{name: "cross-seed-link", count: 2281, tags: "cross-seed"},
	{name: "movies", count: 1650},
	{name: "tv-hd", count: 206, tags: "tv"},
}

var benchmarkTorrentsSink []Torrent

// BenchmarkQbitGetTorrents compares the inventory reads of the scope shapes.
// The fake server sends prepared JSON, so the time is the request and the
// decode in go-qbittorrent. The latency cases add a delay to each request,
// as a remote qBittorrent or qui does.
func BenchmarkQbitGetTorrents(b *testing.B) {
	tvCategories := []string{"tv-hd-completed", "tv-hd-completed.cross-seed"}
	wideCategories := []string{"tv-hd-completed", "tv-hd-completed.cross-seed", "cross-seed-link", "tv-hd"}
	scopes := []struct {
		name    string
		scope   domain.InventoryScope
		workers int
	}{
		{name: "no scope"},
		{name: "include 2 categories", scope: domain.InventoryScope{IncludeCategories: tvCategories}},
		{name: "include 2 categories serial", scope: domain.InventoryScope{IncludeCategories: tvCategories}, workers: 1},
		{name: "include 2 categories exclude 1 tag", scope: domain.InventoryScope{IncludeCategories: tvCategories, ExcludeTags: []string{"noreuse"}}},
		{name: "include 1 tag", scope: domain.InventoryScope{IncludeTags: []string{"tv"}}},
		{name: "exclude 1 category", scope: domain.InventoryScope{ExcludeCategories: []string{"movies"}}},
		// The same torrents as "exclude 1 category", through four include reads.
		{name: "include 4 categories", scope: domain.InventoryScope{IncludeCategories: wideCategories}},
		{name: "include 4 categories serial", scope: domain.InventoryScope{IncludeCategories: wideCategories}, workers: 1},
	}
	for _, latency := range []time.Duration{0, 5 * time.Millisecond} {
		server := newBenchmarkQbitServer(b, latency)
		for _, tt := range scopes {
			b.Run(fmt.Sprintf("latency=%s/%s", latency, tt.name), func(b *testing.B) {
				client, err := newQbitClient(b.Context(), &domain.Client{Type: "qbittorrent", Host: server.URL, Inventory: tt.scope})
				if err != nil {
					b.Fatal(err)
				}
				client.inventoryReadWorkers = tt.workers
				// The first read loads the WebAPI version and the prepared responses.
				if _, err := client.GetTorrents(b.Context()); err != nil {
					b.Fatal(err)
				}
				server.sent.Store(0)
				b.ReportAllocs()
				b.ResetTimer()

				var torrents []Torrent
				for b.Loop() {
					torrents, err = client.GetTorrents(b.Context())
					if err != nil {
						b.Fatal(err)
					}
				}
				benchmarkTorrentsSink = torrents
				b.ReportMetric(float64(len(torrents)), "torrents")
				b.ReportMetric(float64(server.sent.Load())/float64(b.N)/1e6, "MB-read/op")
			})
		}
	}
}

type benchmarkQbitServer struct {
	*httptest.Server
	sent atomic.Int64
}

// newBenchmarkQbitServer serves torrents/info with the category and tag
// filters of qBittorrent. It prepares every response before it starts.
func newBenchmarkQbitServer(b *testing.B, latency time.Duration) *benchmarkQbitServer {
	b.Helper()
	var inventory []qbittorrent.Torrent
	filters := []qbittorrent.TorrentFilterOptions{{}}
	for _, category := range benchmarkInventoryCategories {
		filters = append(filters, qbittorrent.TorrentFilterOptions{Category: category.name})
		for index := range category.count {
			inventory = append(inventory, benchmarkQbitTorrent(len(inventory), index, category.name, category.tags))
		}
	}
	for _, tag := range []string{"tv", "cross-seed", "noreuse"} {
		filters = append(filters, qbittorrent.TorrentFilterOptions{Tag: tag})
	}
	responses := make(map[string][]byte, len(filters))
	for _, filter := range filters {
		var selected []qbittorrent.Torrent
		for _, t := range inventory {
			if qbitFilterMatches(filter, t) {
				selected = append(selected, t)
			}
		}
		body, err := json.Marshal(selected)
		if err != nil {
			b.Fatal(err)
		}
		responses[filter.Category+"\x00"+filter.Tag] = body
	}

	server := &benchmarkQbitServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v2/app/webapiVersion", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("2.11.4"))
	})
	mux.HandleFunc("GET /api/v2/torrents/info", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(latency)
		body, ok := responses[r.URL.Query().Get("category")+"\x00"+r.URL.Query().Get("tag")]
		if !ok {
			http.Error(w, "no prepared response", http.StatusBadRequest)
			return
		}
		server.sent.Add(int64(len(body)))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	server.Server = httptest.NewServer(mux)
	b.Cleanup(server.Close)
	return server
}

// benchmarkQbitTorrent fills the fields that make a qBittorrent torrent about 2 KB of JSON.
func benchmarkQbitTorrent(id, index int, category, tags string) qbittorrent.Torrent {
	hash := fmt.Sprintf("%040x", id)
	name := fmt.Sprintf("Show.%04d.S01E%02d.1080p.WEB-DL.DDP5.1.H.264-RlsGrp", index/10, index%10+1)
	if category == "movies" {
		name = fmt.Sprintf("Movie.%04d.2024.1080p.BluRay.x264-RlsGrp", index)
	}
	if index%10 == 0 && tags != "" {
		tags += ", noreuse"
	}
	trackers := strings.Repeat("&tr=https%3A%2F%2Ftracker.example.org%2Fannounce%2F0123456789abcdef0123456789abcdef", 6)
	savePath := "/data/torrents/" + category
	return qbittorrent.Torrent{
		AddedOn:      1_700_000_000 + int64(id),
		Category:     category,
		Comment:      "https://tracker.example.org/torrents/" + hash,
		ContentPath:  savePath + "/" + name,
		CreatedBy:    "mktorrent 1.1",
		Hash:         hash,
		InfohashV1:   hash,
		MagnetURI:    "magnet:?xt=urn:btih:" + hash + "&dn=" + name + trackers,
		Name:         name,
		SavePath:     savePath,
		Size:         1_500_000_000,
		State:        qbittorrent.TorrentStateStalledUp,
		Tags:         tags,
		TotalSize:    1_500_000_000,
		Tracker:      "https://tracker.example.org/announce/0123456789abcdef0123456789abcdef",
		Progress:     1,
		Ratio:        1.25,
		SeedingTime:  86_400,
		TimeActive:   90_000,
		Uploaded:     1_875_000_000,
		Downloaded:   1_500_000_000,
		Availability: -1,
		MaxRatio:     -1,
	}
}
