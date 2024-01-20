// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

import (
	"github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
)

type Entry struct {
	T qbittorrent.Torrent
	R rls.Release
}
