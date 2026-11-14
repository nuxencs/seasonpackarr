// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/errtrace"

	"github.com/autobrr/go-deluge"
)

const (
	delugeDefaultPort = 58846
	delugeTimeout     = 60 * time.Second
)

// delugeAPI is the subset of the Autobrr Deluge client used by this adapter.
// The native Deluge RPC connection permits one request at a time, so every
// call is serialized by delugeClient.mu.
type delugeAPI interface {
	Connect(ctx context.Context) error
	Close() error
	SessionState(ctx context.Context) ([]string, error)
	TorrentsStatus(ctx context.Context, state deluge.TorrentState, ids []string) (map[string]*deluge.TorrentStatus, error)
	TorrentStatus(ctx context.Context, id string) (*deluge.TorrentStatus, error)
	AddTorrentFile(ctx context.Context, fileName, fileContentBase64 string, options *deluge.Options) (string, error)
	ResumeTorrents(ctx context.Context, ids ...string) error
}

type delugeLabelAPI interface {
	GetLabels(ctx context.Context) ([]string, error)
	SetTorrentLabel(ctx context.Context, hash, label string) error
	AddLabel(ctx context.Context, label string) error
}

type delugeLabelPluginFactory func(context.Context) (delugeLabelAPI, error)

type delugeClient struct {
	c      delugeAPI
	policy domain.ImportPolicy
	mu     sync.Mutex
	label  delugeLabelPluginFactory
	v1     bool

	// lost is true after a call failed below the RPC layer. go-deluge never
	// dials again, and a late response would answer the next request, so the
	// next call must connect again first. mu guards it.
	lost bool

	// pollInterval is a field so tests can shrink it to milliseconds.
	pollInterval time.Duration
}

func newDelugeClient(ctx context.Context, client *domain.Client) (*delugeClient, error) {
	settings, err := buildDelugeSettings(client)
	if err != nil {
		return nil, fmt.Errorf("failed to build deluge settings: %w", err)
	}

	var c delugeAPI
	var label delugeLabelPluginFactory
	switch canonicalType(client.Type) {
	case "deluge-v1":
		raw := deluge.NewV1(settings)
		c = raw
		// Deluge v1 returns an empty status dictionary for an unknown ID.
		// go-deluge cannot decode that dictionary, so GetFiles must filter
		// unknown IDs through SessionState before its bulk status request.
		label = func(ctx context.Context) (delugeLabelAPI, error) {
			return raw.LabelPlugin(ctx)
		}
	case "deluge-v2":
		raw := deluge.NewV2(settings)
		c = raw
		label = func(ctx context.Context) (delugeLabelAPI, error) {
			return raw.LabelPlugin(ctx)
		}
	default:
		return nil, fmt.Errorf("unsupported deluge client type: %s", client.Type)
	}

	ctx, cancel := context.WithTimeout(ctx, delugeTimeout)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		return nil, errtrace.WithStack(fmt.Errorf("failed to connect to deluge: %w", err))
	}

	return &delugeClient{
		c:            c,
		policy:       client.Import,
		label:        label,
		v1:           client.Type == "deluge-v1",
		pollInterval: 500 * time.Millisecond,
	}, nil
}

func buildDelugeSettings(client *domain.Client) (deluge.Settings, error) {
	host := strings.TrimSpace(client.Host)
	if host == "" {
		return deluge.Settings{}, errors.New("host is required")
	}
	if strings.Contains(host, "://") || strings.ContainsAny(host, "/?#") {
		return deluge.Settings{}, errors.New("deluge host must be a hostname or IP address without a URL scheme or path")
	}

	port := client.Port
	if port == 0 {
		port = delugeDefaultPort
	}
	if port < 1 || port > 65535 {
		return deluge.Settings{}, errors.New("deluge port must be between 1 and 65535")
	}

	return deluge.Settings{
		Hostname:         host,
		Port:             uint(port),
		Login:            client.Username,
		Password:         client.Password,
		ReadWriteTimeout: delugeTimeout,
	}, nil
}

// delugeRetry says whether a request can run again on a new connection.
type delugeRetry bool

const (
	// retryOnce is only for a request that is safe to repeat.
	retryOnce delugeRetry = true
	// noRetry is for a request that the daemon can have applied although its
	// response did not arrive.
	noRetry delugeRetry = false
)

// call runs one request on the daemon connection. An error that is not a
// deluge.RPCError means that the connection is lost. The next call then
// connects again first. A retryOnce request also runs once more on the new
// connection.
func (d *delugeClient) call(ctx context.Context, retry delugeRetry, request func() error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.callLocked(ctx, retry, request)
}

func (d *delugeClient) callLocked(ctx context.Context, retry delugeRetry, request func() error) error {
	if d.lost {
		if err := d.reconnect(ctx); err != nil {
			return err
		}
	}

	err := request()
	if err == nil || isDelugeRPCError(err) {
		return err
	}
	d.lost = true
	if retry == noRetry || ctx.Err() != nil || d.reconnect(ctx) != nil {
		return err
	}

	err = request()
	d.lost = err != nil && !isDelugeRPCError(err)
	return err
}

// Close closes the daemon connection when the server drops this client. A
// request that still holds the client connects again on its next call.
func (d *delugeClient) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	wasLost := d.lost
	d.lost = true
	err := d.c.Close()
	if wasLost {
		// A failed reconnect can already have closed the connection.
		return nil
	}
	return err
}

// reconnect closes the old connection first. A request on a closed go-deluge
// connection panics, so lost stays true until a new connection is open.
func (d *delugeClient) reconnect(ctx context.Context) error {
	_ = d.c.Close()
	if err := d.c.Connect(ctx); err != nil {
		return errtrace.WithStack(fmt.Errorf("failed to reconnect to deluge: %w", err))
	}
	d.lost = false
	return nil
}

func isDelugeRPCError(err error) bool {
	_, ok := errors.AsType[deluge.RPCError](err)
	return ok
}

func (d *delugeClient) GetTorrents(ctx context.Context) ([]Torrent, error) {
	ctx, cancel := context.WithTimeout(ctx, delugeTimeout)
	defer cancel()

	var ts map[string]*deluge.TorrentStatus
	err := d.call(ctx, retryOnce, func() (err error) {
		ts, err = d.c.TorrentsStatus(ctx, deluge.StateUnspecified, nil)
		return err
	})
	if err != nil {
		return nil, errtrace.WithStack(fmt.Errorf("failed to get torrents from deluge: %w", err))
	}

	// Deluge returns a map. Sort its keys to keep matching and tests stable.
	hashes := slices.Sorted(maps.Keys(ts))

	torrents := make([]Torrent, 0, len(ts))
	for _, hash := range hashes {
		status := ts[hash]
		if status == nil {
			continue
		}
		statusHash := strings.TrimSpace(status.Hash)
		if statusHash == "" {
			statusHash = hash
		}
		savePath := status.DownloadLocation
		if strings.TrimSpace(savePath) == "" {
			savePath = status.SavePath
		}
		if strings.TrimSpace(savePath) != "" {
			savePath = normalizePath(savePath)
		}
		torrents = append(torrents, Torrent{
			Hash:     statusHash,
			Name:     status.Name,
			SavePath: savePath,
		})
	}

	return torrents, nil
}

func (d *delugeClient) GetFiles(ctx context.Context, hashes []string) []FileResult {
	if len(hashes) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, delugeTimeout)
	defer cancel()

	requestedHashes := uniqueFoldedHashes(hashes)

	// One lock for both requests, so no import changes the session between them.
	d.mu.Lock()
	if d.v1 {
		var sessionHashes []string
		err := d.callLocked(ctx, retryOnce, func() (err error) {
			sessionHashes, err = d.c.SessionState(ctx)
			return err
		})
		if err != nil {
			d.mu.Unlock()
			return fileResultsWithError(hashes, errtrace.WithStack(fmt.Errorf("failed to list deluge torrents: %w", err)))
		}
		knownHashes := make(map[string]struct{}, len(sessionHashes))
		for _, hash := range sessionHashes {
			knownHashes[strings.ToLower(hash)] = struct{}{}
		}
		requestedHashes = slices.DeleteFunc(requestedHashes, func(hash string) bool {
			_, ok := knownHashes[strings.ToLower(hash)]
			return !ok
		})
	}

	var statuses map[string]*deluge.TorrentStatus
	var err error
	if len(requestedHashes) != 0 {
		err = d.callLocked(ctx, retryOnce, func() (callErr error) {
			statuses, callErr = d.c.TorrentsStatus(ctx, deluge.StateUnspecified, requestedHashes)
			return callErr
		})
	}
	d.mu.Unlock()
	if err != nil {
		return fileResultsWithError(hashes, errtrace.WithStack(fmt.Errorf("failed to get files from deluge: %w", err)))
	}

	statusByHash := make(map[string]*deluge.TorrentStatus, len(statuses))
	for _, status := range statuses {
		if status == nil || strings.TrimSpace(status.Hash) == "" {
			continue
		}
		statusByHash[strings.ToLower(status.Hash)] = status
	}

	results := make([]FileResult, len(hashes))
	for index, hash := range hashes {
		results[index].Hash = hash
		status, ok := statusByHash[strings.ToLower(hash)]
		if !ok {
			results[index].Err = fmt.Errorf("deluge: torrent not found: %s", hash)
			continue
		}

		files := make([]File, 0, len(status.Files))
		for _, file := range status.Files {
			files = append(files, File{Name: file.Path, Size: file.Size})
		}
		results[index].Files = files
	}
	return results
}

func uniqueFoldedHashes(hashes []string) []string {
	seen := make(map[string]struct{}, len(hashes))
	unique := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		key := strings.ToLower(hash)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, hash)
	}
	return unique
}

// ImportDestination uses an explicit path because go-deluge does not expose
// Deluge's global download_location setting. Deluge stores multi-file torrent
// paths below their torrent root, so the destination is rooted.
func (d *delugeClient) ImportDestination(ctx context.Context) (ImportDestination, error) {
	if err := ctx.Err(); err != nil {
		return ImportDestination{}, err
	}
	savePath := strings.TrimSpace(d.policy.SavePath)
	if savePath == "" {
		return ImportDestination{}, importErr(ImportStageConfig, errors.New("deluge requires import.savePath"))
	}
	return NewRootedImportDestination(normalizePath(savePath)), nil
}

// Import adds the pack stopped, applies its optional label, then resumes it.
// Deluge/libtorrent performs its normal initial data check before it transfers
// pieces. The adapter waits only until the resume takes effect, not for the
// check, and returns the duration of each client operation. It does not use
// Deluge 2 seed mode: when libtorrent rejects a seed-mode add, Deluge puts the
// torrent in an error state that resume cannot clear.
func (d *delugeClient) Import(ctx context.Context, req ImportRequest) (ImportReport, error) {
	var report ImportReport
	started := time.Now()
	if !req.HasV1 || strings.TrimSpace(req.LegacyHash) == "" {
		report.record(ImportStageConfig, started)
		return report, importErr(ImportStageConfig, errors.New("deluge requires a v1 or hybrid torrent"))
	}

	savePath := strings.TrimSpace(req.SavePath)
	if savePath == "" {
		report.record(ImportStageConfig, started)
		return report, importErr(ImportStageConfig, errors.New("resolved deluge save path is empty"))
	}

	ctx, cancel := context.WithTimeout(ctx, delugeTimeout)
	defer cancel()

	options := &deluge.Options{
		DownloadLocation: new(savePath),
		AddPaused:        new(true),
	}
	content := base64.StdEncoding.EncodeToString(req.TorrentBytes)
	report.record(ImportStageConfig, started)

	started = time.Now()
	var addedHash string
	err := d.call(ctx, noRetry, func() (err error) {
		addedHash, err = d.c.AddTorrentFile(ctx, req.LegacyHash+".torrent", content, options)
		return err
	})
	if isDelugeAlreadyAdded(err) {
		report.record(ImportStageAdd, started)
		return report, nil
	}
	if err != nil {
		report.record(ImportStageAdd, started)
		return report, importErr(ImportStageAdd, fmt.Errorf("failed to add torrent to deluge: %w", err))
	}
	if strings.TrimSpace(addedHash) == "" {
		report.record(ImportStageAdd, started)
		return report, nil
	}
	if err := d.applyLabel(ctx, addedHash); err != nil {
		report.record(ImportStageAdd, started)
		return report, importErr(ImportStageAdd, err)
	}
	report.record(ImportStageAdd, started)

	started = time.Now()
	// A second resume of a started torrent changes nothing.
	err = d.call(ctx, retryOnce, func() error {
		return d.c.ResumeTorrents(ctx, addedHash)
	})
	if err != nil {
		report.record(ImportStageResume, started)
		return report, importErr(ImportStageResume, fmt.Errorf("failed to resume torrent in deluge: %w", err))
	}
	err = d.waitForStarted(ctx, addedHash)
	report.record(ImportStageResume, started)
	if err != nil {
		return report, importErr(ImportStageResume, err)
	}

	return report, nil
}

func (d *delugeClient) applyLabel(ctx context.Context, hash string) error {
	if len(d.policy.Tags) == 0 || d.label == nil {
		return nil
	}

	label := strings.ToLower(strings.TrimSpace(d.policy.Tags[0]))
	if label == "" {
		return nil
	}

	// One call for the plugin requests, so no other request runs between them.
	return d.call(ctx, noRetry, func() error {
		plugin, err := d.label(ctx)
		if err != nil {
			return fmt.Errorf("failed to load deluge Label plugin: %w", err)
		}
		// Match Autobrr's disabled-plugin behavior. When enabled, create the label
		// definition if it does not exist, then assign it to the torrent.
		if plugin == nil {
			return nil
		}
		if err := ensureDelugeLabel(ctx, plugin, hash, label); err != nil {
			return fmt.Errorf("failed to assign deluge label %q: %w", label, err)
		}
		return nil
	})
}

func ensureDelugeLabel(ctx context.Context, plugin delugeLabelAPI, hash, label string) error {
	labels, err := plugin.GetLabels(ctx)
	if err != nil {
		return err
	}
	if !slices.Contains(labels, label) {
		if err := plugin.AddLabel(ctx, label); err != nil {
			return err
		}
	}
	return plugin.SetTorrentLabel(ctx, hash, label)
}

func isDelugeAlreadyAdded(err error) bool {
	rpcErr, ok := errors.AsType[deluge.RPCError](err)
	return ok &&
		rpcErr.ExceptionType == "AddTorrentError" &&
		strings.Contains(strings.ToLower(rpcErr.ExceptionMessage), "already in session")
}

// waitForStarted waits until the resume takes effect. A checking torrent counts
// as started: libtorrent continues into the transfer after its check, and the
// check can take longer than the caller's request timeout.
func (d *delugeClient) waitForStarted(ctx context.Context, hash string) error {
	return pollUntil(ctx, d.pollInterval, func() (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}

		var status *deluge.TorrentStatus
		err := d.call(ctx, retryOnce, func() (err error) {
			status, err = d.c.TorrentStatus(ctx, hash)
			return err
		})
		if err != nil {
			return false, err
		}
		if status == nil {
			return false, nil
		}

		switch deluge.TorrentState(status.State) {
		case deluge.StatePaused:
			return false, nil
		case deluge.StateError:
			return false, fmt.Errorf("deluge reported an error while starting torrent %s", hash)
		default:
			return true, nil
		}
	})
}
