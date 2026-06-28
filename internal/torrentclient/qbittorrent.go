// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"github.com/autobrr/go-qbittorrent"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/pkg/errors"
)

type qbitClient struct {
	c *qbittorrent.Client
}

func newQbitClient(client *domain.Client) (*qbitClient, error) {
	host, err := buildHost(client)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build host")
	}

	c := qbittorrent.NewClient(qbittorrent.Config{
		Host:     host,
		Username: client.Username,
		Password: client.Password,
		APIKey:   client.APIKey,
	})

	if err := c.Login(); err != nil {
		return nil, errors.Wrap(err, "failed to login to qbittorrent")
	}

	return &qbitClient{c: c}, nil
}

func (q *qbitClient) GetTorrents() ([]Torrent, error) {
	ts, err := q.c.GetTorrents(qbittorrent.TorrentFilterOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get torrents")
	}

	torrents := make([]Torrent, 0, len(ts))
	for _, t := range ts {
		torrents = append(torrents, Torrent{
			Hash:     t.Hash,
			Name:     t.Name,
			SavePath: t.SavePath,
		})
	}

	return torrents, nil
}

func (q *qbitClient) GetFiles(hash string) ([]File, error) {
	torrentFiles, err := q.c.GetFilesInformation(hash)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get files")
	}

	files := make([]File, 0, len(*torrentFiles))
	for _, f := range *torrentFiles {
		files = append(files, File{
			Name: f.Name,
			Size: f.Size,
		})
	}

	return files, nil
}
