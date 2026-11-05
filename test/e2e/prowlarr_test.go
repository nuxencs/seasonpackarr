// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build e2e

package e2e

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/daemontest"
	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/stretchr/testify/require"
)

const (
	envProwlarrURL    = "SEASONPACKARR_TEST_E2E_PROWLARR_URL"
	envProwlarrAPIKey = "SEASONPACKARR_TEST_E2E_PROWLARR_API_KEY"
	// torznabCategory is the TV/HD category of the fake Torznab feed.
	// seasonpackarr searches the TV category when an indexer has one.
	torznabCategory = 5040
)

var (
	prowlarrOnce sync.Once
	prowlarrErr  error
)

// requireProwlarr returns the shared harness with the Generic Torznab indexer
// in Prowlarr. Only the e2e-search entry starts Prowlarr.
func requireProwlarr(t *testing.T) *harness {
	t.Helper()
	daemontest.RequireSettings(t, envProwlarrURL, envProwlarrAPIKey)
	h := requireHarness(t)
	prowlarrOnce.Do(func() { prowlarrErr = h.createTorznabIndexer() })
	require.NoError(t, prowlarrErr, "set up Prowlarr")
	return h
}

// createTorznabIndexer adds a Generic Torznab indexer for the fake Torznab feed
// to Prowlarr. Prowlarr reads the capabilities and runs an RSS query before it
// saves the indexer.
func (h *harness) createTorznabIndexer() error {
	api := jsonAPI{
		baseURL:   strings.TrimSuffix(os.Getenv(envProwlarrURL), "/"),
		client:    &http.Client{Timeout: daemontest.Timeout},
		keyHeader: "X-Api-Key",
		apiKey:    os.Getenv(envProwlarrAPIKey),
	}
	var schema []map[string]any
	if err := api.do(http.MethodGet, "/api/v1/indexer/schema", nil, http.StatusOK, &schema); err != nil {
		return fmt.Errorf("read the indexer schema: %w", err)
	}
	index := slices.IndexFunc(schema, func(s map[string]any) bool { return s["name"] == "Generic Torznab" })
	if index < 0 {
		return errors.New("prowlarr has no Generic Torznab indexer")
	}
	indexer := schema[index]
	fields, _ := indexer["fields"].([]any)
	found := false
	for _, field := range fields {
		if field, ok := field.(map[string]any); ok && field["name"] == "baseUrl" {
			field["value"] = h.torrentURL
			found = true
		}
	}
	if !found {
		return errors.New("the Generic Torznab schema has no baseUrl field")
	}
	indexer["name"] = "Seasonpackarr E2E"
	indexer["enable"] = true
	indexer["priority"] = 25
	// The default app profile, which every Prowlarr database has.
	indexer["appProfileId"] = 1
	if err := api.do(http.MethodPost, "/api/v1/indexer", indexer, http.StatusCreated, nil); err != nil {
		return fmt.Errorf("create the Generic Torznab indexer: %w", err)
	}
	return nil
}

// addRelease adds the pack name to the fake Torznab feed and serves torrent
// for it.
func (h *harness) addRelease(t *testing.T, name string, torrent []byte) {
	t.Helper()
	info, err := torrents.Info(torrent)
	require.NoError(t, err, "read the .torrent of %s", name)
	file := name + ".torrent"
	h.torrents.Store(file, torrent)
	h.feedMu.Lock()
	defer h.feedMu.Unlock()
	h.feed = append(h.feed, newTorznabItem(name, h.torrentURL+"/torrents/"+file, info.TotalLength()))
}

// torznabCaps declares a basic search and a TV search with season and episode,
// which Prowlarr requires of a TV search. Prowlarr maps the subcat name HD of
// the parent TV to the standard category 5040.
const torznabCaps = `<?xml version="1.0" encoding="UTF-8"?>
<caps>
  <server title="Seasonpackarr E2E"/>
  <limits default="100" max="100"/>
  <searching>
    <search available="yes" supportedParams="q"/>
    <tv-search available="yes" supportedParams="q,season,ep"/>
  </searching>
  <categories>
    <category id="5000" name="TV">
      <subcat id="5040" name="HD"/>
    </category>
  </categories>
</caps>
`

type torznabFeed struct {
	XMLName xml.Name      `xml:"rss"`
	Version string        `xml:"version,attr"`
	Items   []torznabItem `xml:"channel>item"`
}

// torznabItem has the fields that Prowlarr requires of a Torznab item: a
// publish date, a size in the enclosure length, and the torrent MIME type.
type torznabItem struct {
	Title     string `xml:"title"`
	GUID      string `xml:"guid"`
	Link      string `xml:"link"`
	PubDate   string `xml:"pubDate"`
	Category  int    `xml:"category"`
	Enclosure struct {
		URL    string `xml:"url,attr"`
		Length int64  `xml:"length,attr"`
		Type   string `xml:"type,attr"`
	} `xml:"enclosure"`
}

func newTorznabItem(name, link string, size int64) torznabItem {
	item := torznabItem{Title: name, GUID: link, Link: link, PubDate: time.Now().UTC().Format(time.RFC1123Z), Category: torznabCategory}
	item.Enclosure.URL, item.Enclosure.Length, item.Enclosure.Type = link, size, "application/x-bittorrent"
	return item
}

// serveTorznab answers the Torznab requests of Prowlarr. A search with a title
// gets every release that a test added, and Prowlarr filters them by the title,
// as it does for real trackers. Prowlarr saves an indexer only when its RSS
// query, a search without a title, returns a release. That query gets one
// release that no test adds. seasonpackarr sends no RSS query in the harness.
func (h *harness) serveTorznab(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml")
	query := r.URL.Query()
	switch query.Get("t") {
	case "caps":
		_, _ = w.Write([]byte(torznabCaps))
	case "search", "tvsearch":
		feed := torznabFeed{Version: "2.0"}
		if query.Get("q") == "" {
			feed.Items = []torznabItem{newTorznabItem("Seasonpackarr.Harness.Rss.S01.1080p.WEB-DL.H.264-RlsGrp", h.torrentURL+"/torrents/rss.torrent", 1)}
		} else {
			h.feedMu.Lock()
			feed.Items = slices.Clone(h.feed)
			h.feedMu.Unlock()
		}
		_, _ = w.Write([]byte(xml.Header))
		_ = xml.NewEncoder(w).Encode(feed)
	default:
		_, _ = fmt.Fprint(w, `<error code="202" description="No such function"/>`)
	}
}
