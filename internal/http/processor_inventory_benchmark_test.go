// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"

	"github.com/puzpuzpuz/xsync/v3"
)

const inventoryBenchmarkClientName = "benchmark"

var (
	inventoryMapSink   map[string][]entry
	inventoryBucketLen int
	inventorySizes     = []int{1_000, 5_000, 10_000, 50_000}
)

func BenchmarkInventoryColdBuild(b *testing.B) {
	for _, torrentCount := range inventorySizes {
		b.Run(strconv.Itoa(torrentCount), func(b *testing.B) {
			p, _, clientConfig := newInventoryBenchmarkProcessor(torrentCount)
			resetInventoryBenchmarkCache(b)
			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				entryMap.Delete(inventoryBenchmarkClientName)
				entries, err := p.getAllTorrents(b.Context(), inventoryBenchmarkClientName, &clientConfig, domain.FuzzyMatching{})
				if err != nil {
					b.Fatal(err)
				}
				inventoryMapSink = entries
			}
		})
	}
}

func BenchmarkInventoryWarmRefresh(b *testing.B) {
	for _, torrentCount := range inventorySizes {
		b.Run(strconv.Itoa(torrentCount), func(b *testing.B) {
			p, _, clientConfig := newInventoryBenchmarkProcessor(torrentCount)
			resetInventoryBenchmarkCache(b)
			if _, err := p.getAllTorrents(b.Context(), inventoryBenchmarkClientName, &clientConfig, domain.FuzzyMatching{}); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				cached, ok := entryMap.Load(inventoryBenchmarkClientName)
				if !ok {
					b.Fatal("inventory snapshot missing")
				}
				cached.expiresAt = time.Time{}
				entries, err := p.getAllTorrents(b.Context(), inventoryBenchmarkClientName, &clientConfig, domain.FuzzyMatching{})
				if err != nil {
					b.Fatal(err)
				}
				inventoryMapSink = entries
			}
		})
	}
}

func BenchmarkInventoryRefreshChurn(b *testing.B) {
	const torrentCount = 10_000

	for _, churnPercent := range []int{0, 1, 10, 100} {
		b.Run(fmt.Sprintf("%d-percent", churnPercent), func(b *testing.B) {
			changedCount := torrentCount * churnPercent / 100
			inventoryA := inventoryBenchmarkTorrentsWithVariant(torrentCount, changedCount, "A")
			inventoryB := inventoryBenchmarkTorrentsWithVariant(torrentCount, changedCount, "B")
			client := &mockTorrentClient{torrents: inventoryA}
			p := newTestProcessor(client)
			clientConfig := inventoryBenchmarkClientConfig()
			resetInventoryBenchmarkCache(b)
			if _, err := p.getAllTorrents(b.Context(), inventoryBenchmarkClientName, &clientConfig, domain.FuzzyMatching{}); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()

			index := 0
			for b.Loop() {
				if index%2 == 0 {
					client.torrents = inventoryB
				} else {
					client.torrents = inventoryA
				}
				cached, ok := entryMap.Load(inventoryBenchmarkClientName)
				if !ok {
					b.Fatal("inventory snapshot missing")
				}
				cached.expiresAt = time.Time{}
				entries, err := p.getAllTorrents(b.Context(), inventoryBenchmarkClientName, &clientConfig, domain.FuzzyMatching{})
				if err != nil {
					b.Fatal(err)
				}
				inventoryMapSink = entries
				index++
			}
		})
	}
}

func BenchmarkInventoryCachedAccess(b *testing.B) {
	for _, torrentCount := range inventorySizes {
		b.Run(strconv.Itoa(torrentCount), func(b *testing.B) {
			p, _, clientConfig := newInventoryBenchmarkProcessor(torrentCount)
			resetInventoryBenchmarkCache(b)
			if _, err := p.getAllTorrents(b.Context(), inventoryBenchmarkClientName, &clientConfig, domain.FuzzyMatching{}); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				entries, err := p.getAllTorrents(b.Context(), inventoryBenchmarkClientName, &clientConfig, domain.FuzzyMatching{})
				if err != nil {
					b.Fatal(err)
				}
				inventoryMapSink = entries
			}
		})
	}
}

func BenchmarkInventoryTitleLookup(b *testing.B) {
	for _, torrentCount := range inventorySizes {
		b.Run(strconv.Itoa(torrentCount), func(b *testing.B) {
			p, _, clientConfig := newInventoryBenchmarkProcessor(torrentCount)
			resetInventoryBenchmarkCache(b)
			entries, err := p.getAllTorrents(b.Context(), inventoryBenchmarkClientName, &clientConfig, domain.FuzzyMatching{})
			if err != nil {
				b.Fatal(err)
			}
			var title string
			for candidateTitle := range entries {
				title = candidateTitle
				break
			}
			if title == "" {
				b.Fatal("inventory has no title bucket")
			}
			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				inventoryBucketLen = len(entries[title])
			}
		})
	}
}

func TestInventoryRetainedMemory(t *testing.T) {
	torrentCountText := os.Getenv("INVENTORY_TORRENTS")
	if torrentCountText == "" {
		t.Skip("set INVENTORY_TORRENTS")
	}
	torrentCount, err := strconv.Atoi(torrentCountText)
	if err != nil || torrentCount < 1 {
		t.Fatalf("invalid INVENTORY_TORRENTS %q", torrentCountText)
	}

	entryMap = xsync.NewMapOf[string, *entryCache]()
	clientConfig := inventoryBenchmarkClientConfig()
	debug.FreeOSMemory()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	client := &mockTorrentClient{torrents: inventoryBenchmarkTorrents(torrentCount)}
	p := newTestProcessor(client)
	entries, err := p.getAllTorrents(t.Context(), inventoryBenchmarkClientName, &clientConfig, domain.FuzzyMatching{})
	if err != nil {
		t.Fatal(err)
	}
	client.torrents = nil

	debug.FreeOSMemory()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	cached, ok := entryMap.Load(inventoryBenchmarkClientName)
	if !ok {
		t.Fatal("inventory snapshot missing")
	}
	runtime.KeepAlive(entries)
	runtime.KeepAlive(cached)
	runtime.KeepAlive(p)

	t.Logf(
		"INVENTORY_MEM torrents=%d title_buckets=%d parsed_releases=%d heap_bytes=%d heap_objects=%d total_alloc_bytes=%d mallocs=%d",
		torrentCount,
		len(cached.entriesMap),
		len(cached.rlsMap),
		int64(after.HeapAlloc)-int64(before.HeapAlloc),
		int64(after.HeapObjects)-int64(before.HeapObjects),
		after.TotalAlloc-before.TotalAlloc,
		after.Mallocs-before.Mallocs,
	)
}

func newInventoryBenchmarkProcessor(torrentCount int) (*processor, *mockTorrentClient, domain.Client) {
	client := &mockTorrentClient{torrents: inventoryBenchmarkTorrents(torrentCount)}
	return newTestProcessor(client), client, inventoryBenchmarkClientConfig()
}

func inventoryBenchmarkClientConfig() domain.Client {
	return domain.Client{
		Type: "qbittorrent",
		Import: domain.ImportPolicy{
			Category: "tv-hd",
		},
	}
}

func inventoryBenchmarkTorrents(count int) []torrentclient.Torrent {
	return inventoryBenchmarkTorrentsWithVariant(count, 0, "")
}

func inventoryBenchmarkTorrentsWithVariant(count, changedCount int, variant string) []torrentclient.Torrent {
	const episodesPerTitle = 20

	torrents := make([]torrentclient.Torrent, count)
	for index := range torrents {
		title := index / episodesPerTitle
		season := title%12 + 1
		episode := index%episodesPerTitle + 1
		group := "GROUP"
		if index < changedCount {
			group += variant
		}
		torrents[index] = torrentclient.Torrent{
			Hash:     fmt.Sprintf("%040x", index),
			Name:     fmt.Sprintf("Inventory.Show.%08x.S%02dE%02d.1080p.WEB-DL.DDP5.1.H.264-%s", title, season, episode, group),
			SavePath: fmt.Sprintf("/data/tv/inventory-show-%08x/season-%02d", title, season),
		}
	}
	return torrents
}

func resetInventoryBenchmarkCache(tb testing.TB) {
	tb.Helper()
	entryMap = xsync.NewMapOf[string, *entryCache]()
	tb.Cleanup(func() {
		entryMap = xsync.NewMapOf[string, *entryCache]()
	})
}
