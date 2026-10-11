// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package daemontest

import (
	"bytes"
	"os"
	"testing"

	"github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
)

// QbitSeeding reports whether qBittorrent has all data of tor and uploads it.
func QbitSeeding(tor qbittorrent.Torrent) bool {
	return tor.Progress >= 1 &&
		(tor.State == qbittorrent.TorrentStateUploading || tor.State == qbittorrent.TorrentStateStalledUp)
}

// AssertHardlinked checks that imported is the same file as source, so the
// pack reuses the source episode and the client did not rewrite it.
func AssertHardlinked(t *testing.T, source, imported string) {
	t.Helper()
	sourceInfo, err := os.Stat(source)
	if !assert.NoError(t, err, "source episode") {
		return
	}
	importedInfo, err := os.Stat(imported)
	if assert.NoError(t, err, "reused episode is not in the pack") {
		assert.True(t, os.SameFile(sourceInfo, importedInfo), "reused episode %s must keep the inode of its source", imported)
	}
}

// AssertSameBytes checks that the downloaded file has the bytes of the seeded file.
func AssertSameBytes(t *testing.T, seeded, downloaded string) {
	t.Helper()
	want, err := os.ReadFile(seeded)
	if !assert.NoError(t, err, "seeded episode") {
		return
	}
	got, err := os.ReadFile(downloaded)
	if assert.NoError(t, err, "downloaded episode is not in the pack") {
		// bytes.Equal, because a diff of two 1 MiB files is unreadable.
		assert.True(t, bytes.Equal(want, got), "downloaded episode %s does not match the seeder", downloaded)
	}
}
