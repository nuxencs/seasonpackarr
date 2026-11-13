// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/hekmon/transmissionrpc/v3"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/errtrace"
)

const transmissionTimeout = 60 * time.Second

// transmissionAPI is the subset of *transmissionrpc.Client the adapter uses. It
// exists so the import machinery can be unit-tested against a stub.
type transmissionAPI interface {
	TorrentGet(ctx context.Context, fields []string, ids []int64) ([]transmissionrpc.Torrent, error)
	TorrentGetHashes(ctx context.Context, fields []string, hashes []string) ([]transmissionrpc.Torrent, error)
	TorrentAdd(ctx context.Context, payload transmissionrpc.TorrentAddPayload) (transmissionrpc.Torrent, error)
	TorrentVerifyHashes(ctx context.Context, hashes []string) error
	TorrentStartHashes(ctx context.Context, hashes []string) error
	SessionArgumentsGetAll(ctx context.Context) (transmissionrpc.SessionArguments, error)
}

type transmissionClient struct {
	c      transmissionAPI
	policy domain.ImportPolicy

	// timeouts are fields so tests can shrink them to milliseconds.
	verifyTimeout time.Duration
	pollInterval  time.Duration
}

func newTransmissionClient(ctx context.Context, client *domain.Client) (*transmissionClient, error) {
	endpoint, err := buildTransmissionURL(client)
	if err != nil {
		return nil, fmt.Errorf("failed to build transmission URL: %w", err)
	}

	c, err := transmissionrpc.New(endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create transmission client: %w", err)
	}

	// Ping to fail fast on bad host/auth (mirrors the qBittorrent adapter's Login()).
	ctx, cancel := context.WithTimeout(ctx, transmissionTimeout)
	defer cancel()
	if _, err := c.SessionArgumentsGetAll(ctx); err != nil {
		return nil, errtrace.WithStack(fmt.Errorf("failed to connect to transmission: %w", err))
	}

	return &transmissionClient{
		c:             c,
		policy:        client.Import,
		verifyTimeout: 10 * time.Minute,
		pollInterval:  500 * time.Millisecond,
	}, nil
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
		return nil, fmt.Errorf("failed to parse host: %w", err)
	}

	parsed.Path = "/transmission/rpc"
	if client.Username != "" {
		parsed.User = url.UserPassword(client.Username, client.Password)
	}

	return parsed, nil
}

func (t *transmissionClient) GetTorrents(ctx context.Context) ([]Torrent, error) {
	ctx, cancel := context.WithTimeout(ctx, transmissionTimeout)
	defer cancel()

	ts, err := t.c.TorrentGet(ctx, []string{"hashString", "name", "downloadDir"}, nil)
	if err != nil {
		return nil, errtrace.WithStack(fmt.Errorf("failed to get torrents: %w", err))
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

func (t *transmissionClient) GetFiles(ctx context.Context, hashes []string) []FileResult {
	if len(hashes) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, transmissionTimeout)
	defer cancel()

	ts, err := t.c.TorrentGetHashes(ctx, []string{"hashString", "files"}, hashes)
	if err != nil {
		return fileResultsWithError(hashes, errtrace.WithStack(fmt.Errorf("failed to get files: %w", err)))
	}

	filesByHash := make(map[string][]File, len(ts))
	for _, torrent := range ts {
		files := make([]File, 0, len(torrent.Files))
		for _, file := range torrent.Files {
			files = append(files, File{Name: file.Name, Size: file.Length})
		}
		filesByHash[strings.ToLower(derefString(torrent.HashString))] = files
	}

	results := make([]FileResult, len(hashes))
	for index, hash := range hashes {
		results[index].Hash = hash
		files, ok := filesByHash[strings.ToLower(hash)]
		if !ok {
			results[index].Err = fmt.Errorf("transmission: torrent not found: %s", hash)
			continue
		}
		results[index].Files = files
	}
	return results
}

// ImportDestination resolves the final import destination for this transmission client:
// an explicit savePath wins, otherwise the session's default download dir.
func (t *transmissionClient) ImportDestination(ctx context.Context) (ImportDestination, error) {
	if t.policy.SavePath != "" {
		return NewRootedImportDestination(normalizePath(t.policy.SavePath)), nil
	}

	ctx, cancel := context.WithTimeout(ctx, transmissionTimeout)
	defer cancel()

	args, err := t.c.SessionArgumentsGetAll(ctx)
	if err != nil {
		return ImportDestination{}, importErr(ImportStageConfig, errtrace.WithStack(fmt.Errorf("could not read transmission session: %w", err)))
	}

	downloadDir := strings.TrimSpace(derefString(args.DownloadDir))
	if downloadDir == "" {
		return ImportDestination{}, importErr(ImportStageConfig, errors.New("transmission download dir is empty; set import.savePath"))
	}

	return NewRootedImportDestination(normalizePath(downloadDir)), nil
}

// Import adds the parsed season pack to transmission (paused, into the resolved
// import root that already holds the hardlinked episodes), forces a hash
// verification so the present pieces are recognised, waits for it to settle,
// then starts the torrent.
//
// Transmission has no skip-hash-check equivalent (verified against 4.0.6 and
// 4.1.3), so the import always verifies. Only the genuinely missing pieces are
// downloaded once started. The returned report identifies how long each client
// operation took.
func (t *transmissionClient) Import(ctx context.Context, req ImportRequest) (ImportReport, error) {
	var report ImportReport
	started := time.Now()
	if strings.TrimSpace(req.LegacyHash) == "" {
		report.record(ImportStageConfig, started)
		return report, importErr(ImportStageConfig, errors.New("resolved transmission info hash is empty"))
	}

	ctx, cancel := context.WithTimeout(ctx, t.verifyTimeout)
	defer cancel()

	payload := transmissionrpc.TorrentAddPayload{
		MetaInfo:    new(base64.StdEncoding.EncodeToString(req.TorrentBytes)),
		DownloadDir: new(req.SavePath),
		Paused:      new(true),
	}
	if labels := trimStrings(t.policy.Tags); len(labels) > 0 {
		payload.Labels = labels
	}
	report.record(ImportStageConfig, started)

	started = time.Now()
	if _, err := t.c.TorrentAdd(ctx, payload); err != nil {
		report.record(ImportStageAdd, started)
		return report, importErr(ImportStageAdd, fmt.Errorf("failed to add torrent to transmission: %w", err))
	}
	report.record(ImportStageAdd, started)

	started = time.Now()
	if err := t.c.TorrentVerifyHashes(ctx, []string{req.LegacyHash}); err != nil {
		report.record(ImportStageRecheck, started)
		return report, importErr(ImportStageRecheck, fmt.Errorf("failed to verify torrent: %w", err))
	}

	if err := t.waitForVerify(ctx, req.LegacyHash); err != nil {
		report.record(ImportStageRecheck, started)
		return report, importErr(ImportStageRecheck, err)
	}
	report.record(ImportStageRecheck, started)

	// a correctly imported torrent always starts once verification has settled
	started = time.Now()
	if err := t.c.TorrentStartHashes(ctx, []string{req.LegacyHash}); err != nil {
		report.record(ImportStageResume, started)
		return report, importErr(ImportStageResume, fmt.Errorf("failed to start torrent: %w", err))
	}
	report.record(ImportStageResume, started)

	return report, nil
}

// waitForVerify polls until the torrent leaves the checking states. Because the
// torrent was added paused, it settles back to stopped once verification
// finishes. Completion is detected by "no longer checking" rather than by an
// observed CHECK state, since a small partial verify can pass through checking
// faster than the poll interval.
func (t *transmissionClient) waitForVerify(ctx context.Context, hash string) error {
	return pollUntil(ctx, t.pollInterval, func() (bool, error) {
		ts, err := t.c.TorrentGetHashes(ctx, []string{"status", "percentDone", "recheckProgress", "errorString"}, []string{hash})
		if err != nil {
			return false, err
		}
		if len(ts) == 0 {
			return false, nil
		}

		tr := ts[0]
		if es := strings.TrimSpace(derefString(tr.ErrorString)); es != "" {
			return false, fmt.Errorf("transmission reported error: %s", es)
		}

		if tr.Status == nil {
			return false, nil
		}
		switch *tr.Status {
		case transmissionrpc.TorrentStatusCheckWait, transmissionrpc.TorrentStatusCheck:
			return false, nil
		default:
			return true, nil
		}
	})
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func trimStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s := strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
