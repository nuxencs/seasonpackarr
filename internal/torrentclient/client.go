// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/pkg/errors"
)

// Torrent is the neutral view of a torrent shared across client implementations.
//
// SavePath MUST be the absolute on-disk directory the torrent's content was
// downloaded into. Together with File.Name it forms the hardlink source path
// (see TorrentClient).
type Torrent struct {
	Hash     string
	Name     string
	SavePath string
}

// File is the neutral view of a single file within a torrent.
//
// Name MUST be the file path relative to the owning Torrent.SavePath, including
// the torrent's root folder (e.g. "Show.S01/Show.S01E01.mkv" for a multi-file
// torrent). This is load-bearing: the processor hardlinks
// filepath.Join(Torrent.SavePath, File.Name), so returning a bare basename here
// would silently break hardlinking.
type File struct {
	Name string
	Size int64
}

// TorrentClient is the minimal surface the processor needs from a torrent client.
//
// Contract for implementations: the values returned must satisfy
// filepath.Join(Torrent.SavePath, File.Name) == the real absolute on-disk path
// of the file, so it can be used directly as a hardlink source. The hash
// returned by GetTorrents must be accepted as-is by GetFiles.
type TorrentClient interface {
	GetTorrents() ([]Torrent, error)
	GetFiles(hash string) ([]File, error)
}

func New(client *domain.Client) (TorrentClient, error) {
	switch client.Type {
	case "", "qbittorrent":
		return newQbitClient(client)
	case "transmission":
		return newTransmissionClient(client)
	default:
		return nil, fmt.Errorf("unknown client type: %s", client.Type)
	}
}

func buildHost(client *domain.Client) (string, error) {
	if len(client.Host) == 0 {
		return "", errors.New("host is required")
	}

	host := client.Host
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}

	parsedURL, err := url.Parse(host)
	if err != nil {
		return "", errors.Wrap(err, "failed to parse host")
	}

	if client.Port != 0 {
		parsedURL.Host = net.JoinHostPort(parsedURL.Hostname(), strconv.Itoa(client.Port))
	}

	return parsedURL.String(), nil
}
