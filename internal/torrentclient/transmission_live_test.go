// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"os"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"
)

// TestTransmissionLive exercises the full adapter against a real Transmission instance.
//
// Opt in by setting SEASONPACKARR_TEST_TRANSMISSION_HOST to the base URL of the
// Transmission web UI (e.g. "https://transmission.example.com" or "192.168.1.10:9091").
// Credentials are supplied separately via SEASONPACKARR_TEST_TRANSMISSION_USER and
// SEASONPACKARR_TEST_TRANSMISSION_PASS. The test is skipped when the host var is unset.
func TestTransmissionLive(t *testing.T) {
	host := os.Getenv("SEASONPACKARR_TEST_TRANSMISSION_HOST")
	if host == "" {
		t.Skip("SEASONPACKARR_TEST_TRANSMISSION_HOST not set — skipping live smoke test")
	}

	c, err := newTransmissionClient(t.Context(), &domain.Client{
		Host:     host,
		Username: os.Getenv("SEASONPACKARR_TEST_TRANSMISSION_USER"),
		Password: os.Getenv("SEASONPACKARR_TEST_TRANSMISSION_PASS"),
	})
	if err != nil {
		t.Fatalf("newTransmissionClient: %v", err)
	}

	torrents, err := c.GetTorrents(t.Context())
	if err != nil {
		t.Fatalf("GetTorrents: %v", err)
	}
	t.Logf("GetTorrents returned %d torrent(s)", len(torrents))

	if len(torrents) == 0 {
		t.Log("no torrents found — skipping GetFiles check")
		return
	}

	first := torrents[0]
	t.Logf("first torrent: hash=%s name=%q savePath=%q", first.Hash, first.Name, first.SavePath)

	results := c.GetFiles(t.Context(), []string{first.Hash})
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("GetFiles(%q): %+v", first.Hash, results)
	}
	files := results[0].Files
	t.Logf("GetFiles returned %d file(s)", len(files))
	for i, f := range files {
		t.Logf("  [%d] name=%q size=%d", i, f.Name, f.Size)
	}
}
