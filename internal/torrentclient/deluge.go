// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"context"
	"encoding/base64"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/autobrr/go-deluge"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/pkg/errors"
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

	// timeouts are fields so tests can shrink them to milliseconds.
	checkTimeout time.Duration
	pollInterval time.Duration
}

func newDelugeClient(client *domain.Client) (*delugeClient, error) {
	settings, err := buildDelugeSettings(client)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build deluge settings")
	}

	var c delugeAPI
	var label delugeLabelPluginFactory
	switch client.Type {
	case "deluge-v1":
		raw := deluge.NewV1(settings)
		c = raw
		// Deluge v1 returns an empty status dictionary for an unknown ID.
		// go-deluge cannot decode that dictionary, so GetFiles must filter
		// unknown IDs through SessionState before its bulk status request.
		label = func(ctx context.Context) (delugeLabelAPI, error) {
			return raw.LabelPlugin(ctx)
		}
	case "deluge", "deluge-v2":
		raw := deluge.NewV2(settings)
		c = raw
		label = func(ctx context.Context) (delugeLabelAPI, error) {
			return raw.LabelPlugin(ctx)
		}
	default:
		return nil, errors.New("unsupported deluge client type: %s", client.Type)
	}

	ctx, cancel := context.WithTimeout(context.Background(), delugeTimeout)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		return nil, errors.Wrap(err, "failed to connect to deluge")
	}

	return &delugeClient{
		c:            c,
		policy:       client.Import,
		label:        label,
		v1:           client.Type == "deluge-v1",
		checkTimeout: 10 * time.Minute,
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

func (d *delugeClient) GetTorrents() ([]Torrent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), delugeTimeout)
	defer cancel()

	d.mu.Lock()
	ts, err := d.c.TorrentsStatus(ctx, deluge.StateUnspecified, nil)
	d.mu.Unlock()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get torrents from deluge")
	}

	// Deluge returns a map. Sort its keys to keep matching and tests stable.
	hashes := make([]string, 0, len(ts))
	for hash := range ts {
		hashes = append(hashes, hash)
	}
	sort.Strings(hashes)

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

func (d *delugeClient) GetFiles(hashes []string) []FileResult {
	if len(hashes) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), delugeTimeout)
	defer cancel()

	requestedHashes := uniqueFoldedHashes(hashes)

	d.mu.Lock()
	if d.v1 {
		sessionHashes, err := d.c.SessionState(ctx)
		if err != nil {
			d.mu.Unlock()
			return fileResultsWithError(hashes, errors.Wrap(err, "failed to list deluge torrents"))
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
		statuses, err = d.c.TorrentsStatus(ctx, deluge.StateUnspecified, requestedHashes)
	}
	d.mu.Unlock()
	if err != nil {
		return fileResultsWithError(hashes, errors.Wrap(err, "failed to get files from deluge"))
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
func (d *delugeClient) ImportDestination() (ImportDestination, error) {
	savePath := strings.TrimSpace(d.policy.SavePath)
	if savePath == "" {
		return ImportDestination{}, importErr(ImportStageConfig, errors.New("deluge requires import.savePath"))
	}
	return NewRootedImportDestination(normalizePath(savePath)), nil
}

// Import adds the pack stopped, applies its optional label, then resumes it.
// Deluge/libtorrent performs its normal initial data check before it transfers
// pieces. The adapter waits until the torrent is no longer paused or checking
// and returns the duration of each client operation.
func (d *delugeClient) Import(req ImportRequest) (ImportReport, error) {
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

	ctx, cancel := context.WithTimeout(context.Background(), d.checkTimeout)
	defer cancel()

	paused := true
	options := &deluge.Options{
		DownloadLocation: &savePath,
		AddPaused:        &paused,
	}
	content := base64.StdEncoding.EncodeToString(req.TorrentBytes)
	report.record(ImportStageConfig, started)

	started = time.Now()
	d.mu.Lock()
	addedHash, err := d.c.AddTorrentFile(ctx, req.LegacyHash+".torrent", content, options)
	d.mu.Unlock()
	if isDelugeAlreadyAdded(err) {
		report.record(ImportStageAdd, started)
		return report, nil
	}
	if err != nil {
		report.record(ImportStageAdd, started)
		return report, importErr(ImportStageAdd, errors.Wrap(err, "failed to add torrent to deluge"))
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
	d.mu.Lock()
	err = d.c.ResumeTorrents(ctx, addedHash)
	d.mu.Unlock()
	report.record(ImportStageResume, started)
	if err != nil {
		return report, importErr(ImportStageResume, errors.Wrap(err, "failed to resume torrent in deluge"))
	}

	started = time.Now()
	if err := d.waitForStarted(ctx, addedHash); err != nil {
		report.record(ImportStageRecheck, started)
		return report, importErr(ImportStageRecheck, err)
	}
	report.record(ImportStageRecheck, started)

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

	d.mu.Lock()
	defer d.mu.Unlock()

	plugin, err := d.label(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to load deluge Label plugin")
	}
	// Match Autobrr's disabled-plugin behavior. When enabled, create the label
	// definition if it does not exist, then assign it to the torrent.
	if plugin == nil {
		return nil
	}
	if err := ensureDelugeLabel(ctx, plugin, hash, label); err != nil {
		return errors.Wrap(err, "failed to assign deluge label %q", label)
	}
	return nil
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
	var rpcErr deluge.RPCError
	return errors.As(err, &rpcErr) &&
		rpcErr.ExceptionType == "AddTorrentError" &&
		strings.Contains(strings.ToLower(rpcErr.ExceptionMessage), "already in session")
}

func (d *delugeClient) waitForStarted(ctx context.Context, hash string) error {
	return pollUntil(d.pollInterval, d.checkTimeout, func() (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}

		d.mu.Lock()
		status, err := d.c.TorrentStatus(ctx, hash)
		d.mu.Unlock()
		if err != nil {
			return false, err
		}
		if status == nil {
			return false, nil
		}

		switch deluge.TorrentState(status.State) {
		case deluge.StatePaused, deluge.StateChecking, deluge.StateAllocating, deluge.StateMoving:
			return false, nil
		case deluge.StateError:
			return false, errors.New("deluge reported an error while starting torrent %s", hash)
		default:
			return true, nil
		}
	})
}
