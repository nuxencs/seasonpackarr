// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/hekmon/transmissionrpc/v3"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/pkg/errors"
)

const transmissionTimeout = 60 * time.Second

type transmissionClient struct {
	c *transmissionrpc.Client
}

func newTransmissionClient(client *domain.Client) (*transmissionClient, error) {
	endpoint, err := buildTransmissionURL(client)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build transmission url")
	}

	c, err := transmissionrpc.New(endpoint, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create transmission client")
	}

	// Ping to fail fast on bad host/auth (mirrors the qBittorrent adapter's Login()).
	ctx, cancel := context.WithTimeout(context.Background(), transmissionTimeout)
	defer cancel()
	if _, err := c.SessionArgumentsGetAll(ctx); err != nil {
		return nil, errors.Wrap(err, "failed to connect to transmission")
	}

	return &transmissionClient{c: c}, nil
}

// buildTransmissionURL builds the Transmission RPC endpoint URL. Basic auth
// credentials are embedded as user info so the underlying library sends them on
// every request; an empty username leaves the request unauthenticated.
func buildTransmissionURL(client *domain.Client) (*url.URL, error) {
	host, err := buildHost(client)
	if err != nil {
		return nil, err
	}

	parsed, err := url.Parse(host)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse host")
	}

	parsed.Path = "/transmission/rpc"
	if client.Username != "" {
		parsed.User = url.UserPassword(client.Username, client.Password)
	}

	return parsed, nil
}

func (t *transmissionClient) GetTorrents() ([]Torrent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), transmissionTimeout)
	defer cancel()

	ts, err := t.c.TorrentGet(ctx, []string{"hashString", "name", "downloadDir"}, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get torrents")
	}

	torrents := make([]Torrent, 0, len(ts))
	for _, tr := range ts {
		torrents = append(torrents, Torrent{
			Hash:     derefString(tr.HashString),
			Name:     derefString(tr.Name),
			SavePath: derefString(tr.DownloadDir),
		})
	}

	return torrents, nil
}

func (t *transmissionClient) GetFiles(hash string) ([]File, error) {
	ctx, cancel := context.WithTimeout(context.Background(), transmissionTimeout)
	defer cancel()

	ts, err := t.c.TorrentGetHashes(ctx, []string{"files"}, []string{hash})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get files")
	}

	if len(ts) == 0 {
		return nil, fmt.Errorf("transmission: torrent not found: %s", hash)
	}

	rawFiles := ts[0].Files
	files := make([]File, 0, len(rawFiles))
	for _, f := range rawFiles {
		files = append(files, File{
			Name: f.Name,
			Size: f.Length,
		})
	}

	return files, nil
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
