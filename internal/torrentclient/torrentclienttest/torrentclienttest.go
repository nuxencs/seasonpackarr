// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package torrentclienttest provides a fake torrentclient.TorrentClient for
// tests that need no live torrent client.
package torrentclienttest

import (
	"context"
	"slices"

	"github.com/nuxencs/seasonpackarr/internal/placement"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
)

// Client is a configurable in-memory torrentclient.TorrentClient. The zero
// value holds no torrents. It has no lock: a test that calls it from several
// goroutines must order the calls itself.
type Client struct {
	Torrents        []torrentclient.Torrent
	TorrentsErr     error
	GetTorrentsFunc func(context.Context) ([]torrentclient.Torrent, error) // replaces Torrents and TorrentsErr when set
	TorrentCalls    int
	Files           []torrentclient.File            // returned for any hash when FilesByHash is nil
	FilesByHash     map[string][]torrentclient.File // per-hash file lists
	FilesErr        error
	FileErrByHash   map[string]error
	GotHash         string   // first hash of the last GetFiles call
	GotHashes       []string // hashes of the last GetFiles call
	FileCalls       int      // hashes over all GetFiles calls
	FileBatchCalls  int      // GetFiles calls
	AfterGetFiles   func()

	ImportRoot          string
	ImportRootErr       error
	FlatImport          bool // ImportDestination returns a flat destination
	OnImportDestination func()
	ImportErr           error
	ImportReport        torrentclient.ImportReport
	ImportCalled        bool
	ImportCalls         int
	ImportReq           torrentclient.ImportRequest // request of the last Import call
	ImportCtxErr        error                       // ctx.Err() at the last Import call
}

var _ torrentclient.TorrentClient = (*Client)(nil)

func (f *Client) GetTorrents(ctx context.Context) ([]torrentclient.Torrent, error) {
	f.TorrentCalls++
	if f.GetTorrentsFunc != nil {
		return f.GetTorrentsFunc(ctx)
	}
	return f.Torrents, f.TorrentsErr
}

func (f *Client) GetFiles(_ context.Context, hashes []string) []torrentclient.FileResult {
	f.FileBatchCalls++
	f.FileCalls += len(hashes)
	f.GotHashes = slices.Clone(hashes)
	if len(hashes) > 0 {
		f.GotHash = hashes[0]
	}

	results := make([]torrentclient.FileResult, len(hashes))
	for index, hash := range hashes {
		results[index].Hash = hash
		if f.FilesErr != nil {
			results[index].Err = f.FilesErr
			continue
		}
		if err := f.FileErrByHash[hash]; err != nil {
			results[index].Err = err
			continue
		}
		if f.FilesByHash != nil {
			results[index].Files = f.FilesByHash[hash]
			continue
		}
		results[index].Files = f.Files
	}
	if f.AfterGetFiles != nil {
		f.AfterGetFiles()
	}
	return results
}

func (f *Client) ImportDestination(context.Context) (placement.Destination, error) {
	if f.OnImportDestination != nil {
		f.OnImportDestination()
	}
	if f.ImportRootErr != nil {
		return placement.Destination{}, f.ImportRootErr
	}
	if f.FlatImport {
		return placement.FlatDestination(f.ImportRoot), nil
	}
	return placement.PackFolderDestination(f.ImportRoot), nil
}

func (f *Client) Import(ctx context.Context, req torrentclient.ImportRequest) (torrentclient.ImportReport, error) {
	f.ImportCalled = true
	f.ImportCalls++
	f.ImportReq = req
	f.ImportCtxErr = ctx.Err()
	return f.ImportReport, f.ImportErr
}
