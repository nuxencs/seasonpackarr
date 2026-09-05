// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/nuxencs/seasonpackarr/internal/torrents"
)

func torrentInput(input string) (string, []byte, error) {
	releaseName := strings.TrimSuffix(filepath.Base(input), ".torrent")
	if filepath.Ext(input) != ".torrent" {
		torrentBytes, err := torrents.TorrentFromRls(releaseName, 5)
		return releaseName, torrentBytes, err
	}

	torrentBytes, err := os.ReadFile(input)
	return releaseName, torrentBytes, err
}
