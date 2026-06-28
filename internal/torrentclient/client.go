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

type Torrent struct {
	Hash     string
	Name     string
	SavePath string
}

type File struct {
	Name string
	Size int64
}

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
