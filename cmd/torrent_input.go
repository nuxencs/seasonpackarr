// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nuxencs/seasonpackarr/internal/torrents"
)

func torrentInput(input string) (string, []byte, error) {
	if !strings.EqualFold(filepath.Ext(input), ".torrent") {
		return "", nil, fmt.Errorf("match and import require a real .torrent file; use seasonpackarr candidate \"<release>\" for a release-name check")
	}
	data, err := os.ReadFile(input)
	if err != nil {
		return "", nil, fmt.Errorf("read torrent file: %w", err)
	}
	info, err := torrents.Info(data)
	if err != nil || strings.TrimSpace(info.BestName()) == "" {
		return "", nil, fmt.Errorf("invalid torrent file %q; provide the original .torrent file from your tracker", input)
	}
	return info.BestName(), data, nil
}
