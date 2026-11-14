// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package inventory

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger/loggertest"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"

	"github.com/autobrr/rls"
	"github.com/stretchr/testify/require"
)

// The benchmark names match the former benchmarks of the HTTP package, so benchstat can compare them.

const inventoryBenchmarkClientName = "benchmark"

var (
	inventorySink        *Inventory
	inventoryReleaseSink *rls.Release
	inventorySizes       = []int{1_000, 5_000, 10_000, 50_000}
)

func BenchmarkInventoryColdBuild(b *testing.B) {
	for _, torrentCount := range inventorySizes {
		b.Run(strconv.Itoa(torrentCount), func(b *testing.B) {
			adapter := &fakeAdapter{torrents: inventoryBenchmarkTorrents(torrentCount)}
			cfg := inventoryBenchmarkConfig()
			b.ReportAllocs()

			for b.Loop() {
				// A new store has no inventory and no parsed names.
				client, err := newBenchmarkStore(adapter).Client(b.Context(), inventoryBenchmarkClientName, cfg)
				if err != nil {
					b.Fatal(err)
				}
				inv, err := client.Inventory(b.Context())
				if err != nil {
					b.Fatal(err)
				}
				inventorySink = inv
			}
		})
	}
}

func BenchmarkInventoryWarmRefresh(b *testing.B) {
	for _, torrentCount := range inventorySizes {
		b.Run(strconv.Itoa(torrentCount), func(b *testing.B) {
			store, client := newBenchmarkClient(b, &fakeAdapter{torrents: inventoryBenchmarkTorrents(torrentCount)})
			b.ReportAllocs()

			for b.Loop() {
				expireInventory(store, inventoryBenchmarkClientName)
				inv, err := client.Inventory(b.Context())
				if err != nil {
					b.Fatal(err)
				}
				inventorySink = inv
			}
		})
	}
}

func BenchmarkInventoryRefreshAfterImport(b *testing.B) {
	for _, torrentCount := range inventorySizes {
		b.Run(strconv.Itoa(torrentCount), func(b *testing.B) {
			store, client := newBenchmarkClient(b, &fakeAdapter{torrents: inventoryBenchmarkTorrents(torrentCount)})
			endpoint := torrentclient.EndpointOf(inventoryBenchmarkClientConfig())
			b.ReportAllocs()

			for b.Loop() {
				store.DiscardEndpoint(endpoint)
				inv, err := client.Inventory(b.Context())
				if err != nil {
					b.Fatal(err)
				}
				inventorySink = inv
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
			adapter := &fakeAdapter{torrents: inventoryA}
			store, client := newBenchmarkClient(b, adapter)
			b.ReportAllocs()

			index := 0
			for b.Loop() {
				if index%2 == 0 {
					adapter.torrents = inventoryB
				} else {
					adapter.torrents = inventoryA
				}
				expireInventory(store, inventoryBenchmarkClientName)
				inv, err := client.Inventory(b.Context())
				if err != nil {
					b.Fatal(err)
				}
				inventorySink = inv
				index++
			}
		})
	}
}

func BenchmarkInventoryCachedAccess(b *testing.B) {
	for _, torrentCount := range inventorySizes {
		b.Run(strconv.Itoa(torrentCount), func(b *testing.B) {
			_, client := newBenchmarkClient(b, &fakeAdapter{torrents: inventoryBenchmarkTorrents(torrentCount)})
			b.ReportAllocs()

			for b.Loop() {
				inv, err := client.Inventory(b.Context())
				if err != nil {
					b.Fatal(err)
				}
				inventorySink = inv
			}
		})
	}
}

func BenchmarkInventoryTitleLookup(b *testing.B) {
	for _, torrentCount := range inventorySizes {
		b.Run(strconv.Itoa(torrentCount), func(b *testing.B) {
			torrents := inventoryBenchmarkTorrents(torrentCount)
			_, client := newBenchmarkClient(b, &fakeAdapter{torrents: torrents})
			inv, err := client.Inventory(b.Context())
			require.NoError(b, err)
			release := rls.ParseString(torrents[0].Name)
			require.NotEmpty(b, slices.Collect(inv.SameTitle(&release)), "inventory has no title group")
			b.ReportAllocs()

			for b.Loop() {
				// Read each entry of the group, as the candidate check does.
				for e := range inv.SameTitle(&release) {
					inventoryReleaseSink = e.Release
				}
			}
		})
	}
}

func TestInventory_RetainedMemory(t *testing.T) {
	torrentCountText := os.Getenv("INVENTORY_TORRENTS")
	if torrentCountText == "" {
		t.Skip("set INVENTORY_TORRENTS")
	}
	torrentCount, err := strconv.Atoi(torrentCountText)
	require.NoError(t, err, "parse INVENTORY_TORRENTS")
	require.Positive(t, torrentCount, "INVENTORY_TORRENTS")

	debug.FreeOSMemory()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	adapter := &fakeAdapter{torrents: inventoryBenchmarkTorrents(torrentCount)}
	store, client := newBenchmarkClient(t, adapter)
	inv, err := client.Inventory(t.Context())
	require.NoError(t, err)
	adapter.torrents = nil

	debug.FreeOSMemory()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	store.mu.Lock()
	names := store.parsed[parsedNamesKeyFor(inventoryBenchmarkClientConfig())]
	store.mu.Unlock()
	require.NotNil(t, names.names, "parsed names missing")
	runtime.KeepAlive(inv)
	runtime.KeepAlive(store)

	t.Logf(
		"INVENTORY_MEM torrents=%d title_buckets=%d parsed_releases=%d heap_bytes=%d heap_objects=%d total_alloc_bytes=%d mallocs=%d",
		torrentCount,
		len(inv.titles),
		len(names.names),
		int64(after.HeapAlloc)-int64(before.HeapAlloc),
		int64(after.HeapObjects)-int64(before.HeapObjects),
		after.TotalAlloc-before.TotalAlloc,
		after.Mallocs-before.Mallocs,
	)
}

// expireInventory ends the validity of the stored inventory of name.
// synctest cannot run a benchmark, so benchmarks cannot wait 30 virtual seconds.
func expireInventory(store *Store, name string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if sl := store.inventories[name]; sl != nil {
		sl.expiresAt = time.Time{}
	}
}

func newBenchmarkStore(adapter torrentclient.TorrentClient) *Store {
	return New(func(context.Context, *domain.Client) (torrentclient.TorrentClient, error) {
		return adapter, nil
	}, loggertest.New())
}

// newBenchmarkClient returns a store and a client handle whose inventory is read once.
func newBenchmarkClient(tb testing.TB, adapter *fakeAdapter) (*Store, *Client) {
	tb.Helper()
	store := newBenchmarkStore(adapter)
	client, err := store.Client(tb.Context(), inventoryBenchmarkClientName, inventoryBenchmarkConfig())
	require.NoError(tb, err)
	_, err = client.Inventory(tb.Context())
	require.NoError(tb, err)
	return store, client
}

func inventoryBenchmarkConfig() domain.Config {
	client := inventoryBenchmarkClientConfig()
	return domain.Config{Clients: map[string]*domain.Client{inventoryBenchmarkClientName: &client}}
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
