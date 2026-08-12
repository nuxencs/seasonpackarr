// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/hekmon/transmissionrpc/v3"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/pkg/errors"
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

func (t *transmissionClient) GetFiles(hashes []string) []FileResult {
	if len(hashes) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), transmissionTimeout)
	defer cancel()

	ts, err := t.c.TorrentGetHashes(ctx, []string{"hashString", "files"}, hashes)
	if err != nil {
		return fileResultsWithError(hashes, errors.Wrap(err, "failed to get files"))
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
func (t *transmissionClient) ImportDestination() (ImportDestination, error) {
	if t.policy.SavePath != "" {
		return NewRootedImportDestination(normalizePath(t.policy.SavePath)), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), transmissionTimeout)
	defer cancel()

	args, err := t.c.SessionArgumentsGetAll(ctx)
	if err != nil {
		return ImportDestination{}, importErr(ImportStageConfig, errors.Wrap(err, "could not read transmission session"))
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
func (t *transmissionClient) Import(req ImportRequest) (ImportReport, error) {
	var report ImportReport
	started := time.Now()
	if strings.TrimSpace(req.LegacyHash) == "" {
		report.record(ImportStageConfig, started)
		return report, importErr(ImportStageConfig, errors.New("resolved transmission info hash is empty"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), t.verifyTimeout)
	defer cancel()

	metaInfo := base64.StdEncoding.EncodeToString(req.TorrentBytes)
	downloadDir := req.SavePath
	paused := true

	payload := transmissionrpc.TorrentAddPayload{
		MetaInfo:    &metaInfo,
		DownloadDir: &downloadDir,
		Paused:      &paused,
	}
	if labels := trimStrings(t.policy.Tags); len(labels) > 0 {
		payload.Labels = labels
	}
	report.record(ImportStageConfig, started)

	started = time.Now()
	if _, err := t.c.TorrentAdd(ctx, payload); err != nil {
		report.record(ImportStageAdd, started)
		return report, importErr(ImportStageAdd, errors.Wrap(err, "failed to add torrent to transmission"))
	}
	report.record(ImportStageAdd, started)

	started = time.Now()
	if err := t.c.TorrentVerifyHashes(ctx, []string{req.LegacyHash}); err != nil {
		report.record(ImportStageRecheck, started)
		return report, importErr(ImportStageRecheck, errors.Wrap(err, "failed to verify torrent"))
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
		return report, importErr(ImportStageResume, errors.Wrap(err, "failed to start torrent"))
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
	return pollUntil(t.pollInterval, t.verifyTimeout, func() (bool, error) {
		ts, err := t.c.TorrentGetHashes(ctx, []string{"status", "percentDone", "recheckProgress", "errorString"}, []string{hash})
		if err != nil {
			return false, err
		}
		if len(ts) == 0 {
			return false, nil
		}

		tr := ts[0]
		if es := strings.TrimSpace(derefString(tr.ErrorString)); es != "" {
			return false, errors.New("transmission reported error: %s", es)
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
