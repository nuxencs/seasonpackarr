// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/autobrr/go-qbittorrent"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/pkg/errors"
)

const qbitFileReadWorkers = 4

// qbitAPI is the subset of *qbittorrent.Client the adapter uses. It exists so
// the import machinery can be unit-tested against a stub without a live client.
type qbitAPI interface {
	GetTorrents(o qbittorrent.TorrentFilterOptions) ([]qbittorrent.Torrent, error)
	GetFilesInformation(hash string) (*qbittorrent.TorrentFiles, error)
	AddTorrentFromMemory(buf []byte, options map[string]string) (*qbittorrent.TorrentAddResponse, error)
	GetCategories() (map[string]qbittorrent.Category, error)
	GetDefaultSavePath() (string, error)
	GetAppPreferences() (qbittorrent.AppPreferences, error)
	Recheck(hashes []string) error
	Resume(hashes []string) error
}

type qbitClient struct {
	c      qbitAPI
	policy domain.ImportPolicy

	// timeouts are fields so tests can shrink them to milliseconds.
	findTimeout    time.Duration
	recheckTimeout time.Duration
	pollInterval   time.Duration
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

	return &qbitClient{
		c:              c,
		policy:         client.Import,
		findTimeout:    15 * time.Second,
		recheckTimeout: 5 * time.Minute,
		pollInterval:   250 * time.Millisecond,
	}, nil
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

func (q *qbitClient) GetFiles(hashes []string) []FileResult {
	results := make([]FileResult, len(hashes))
	if len(hashes) == 0 {
		return results
	}

	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(qbitFileReadWorkers, len(hashes)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				results[index] = q.getFiles(hashes[index])
			}
		}()
	}
	for index := range hashes {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	return results
}

func (q *qbitClient) getFiles(hash string) FileResult {
	torrentFiles, err := q.c.GetFilesInformation(hash)
	if err != nil {
		return FileResult{Hash: hash, Err: errors.Wrap(err, "failed to get files")}
	}

	files := make([]File, 0, len(*torrentFiles))
	for _, file := range *torrentFiles {
		files = append(files, File{Name: file.Name, Size: file.Size})
	}
	return FileResult{Hash: hash, Files: files}
}

// ImportDestination resolves the final import destination for this qBittorrent client.
// An explicit savePath wins. Category-only policy follows qBittorrent's Auto TMM
// and manual category-path preferences.
func (q *qbitClient) ImportDestination() (ImportDestination, error) {
	categoryOnly := strings.TrimSpace(q.policy.SavePath) == "" && strings.TrimSpace(q.policy.DownloadPath) == ""
	contentLayout := strings.TrimSpace(q.policy.ContentLayout)

	var preferences qbittorrent.AppPreferences
	if categoryOnly || contentLayout == "" {
		var err error
		preferences, err = q.c.GetAppPreferences()
		if err != nil {
			return ImportDestination{}, importErr(ImportStageConfig, errors.Wrap(err, "could not read qbittorrent preferences"))
		}
	}

	var actualPath string
	if q.policy.SavePath != "" {
		actualPath = q.policy.SavePath
	} else {
		categories, err := q.c.GetCategories()
		if err != nil {
			return ImportDestination{}, importErr(ImportStageConfig, errors.Wrap(err, "could not read qbittorrent categories"))
		}

		_, ok := categories[q.policy.Category]
		if !ok {
			return ImportDestination{}, importErr(ImportStageConfig, errors.New("qbit category %q was not found in qbittorrent", q.policy.Category))
		}

		useCategoryPath := !categoryOnly || preferences.AutoTmmEnabled || preferences.UseCategoryPathsInManualMode
		if !useCategoryPath {
			defaultSavePath, err := q.c.GetDefaultSavePath()
			if err != nil {
				return ImportDestination{}, importErr(ImportStageConfig, errors.Wrap(err, "could not read qbittorrent default save path"))
			}
			actualPath = defaultSavePath
		} else {
			var err error
			actualPath, err = q.resolveCategorySavePath(q.policy.Category, categories)
			if err != nil {
				return ImportDestination{}, err
			}
		}
	}

	if strings.TrimSpace(actualPath) == "" {
		return ImportDestination{}, importErr(ImportStageConfig, errors.New("resolved qbittorrent destination is empty"))
	}

	if contentLayout == "" {
		contentLayout = preferences.TorrentContentLayout
	}

	resolvedLayout, _, err := resolveContentLayout(contentLayout)
	if err != nil {
		return ImportDestination{}, importErr(ImportStageConfig, err)
	}

	root := normalizePath(actualPath)
	if resolvedLayout == qbittorrent.ContentLayoutSubfolderNone {
		return NewFlatImportDestination(root), nil
	}

	return NewRootedImportDestination(root), nil
}

func (q *qbitClient) resolveCategorySavePath(categoryName string, categories map[string]qbittorrent.Category) (string, error) {
	defaultSavePath := func() (string, error) {
		path, err := q.c.GetDefaultSavePath()
		if err != nil {
			return "", importErr(ImportStageConfig, errors.Wrap(err, "could not read qbittorrent default save path"))
		}
		return path, nil
	}

	var resolve func(string) (string, error)
	resolve = func(name string) (string, error) {
		if name == "" {
			return defaultSavePath()
		}

		categorySavePath := strings.TrimSpace(categories[name].SavePath)
		if categorySavePath != "" {
			if filepath.IsAbs(filepath.FromSlash(categorySavePath)) {
				return categorySavePath, nil
			}
			basePath, err := defaultSavePath()
			if err != nil {
				return "", err
			}
			return filepath.Join(basePath, categorySavePath), nil
		}

		parent, child := "", name
		if separator := strings.LastIndex(name, "/"); separator >= 0 {
			parent = name[:separator]
			child = name[separator+1:]
		}

		basePath, err := resolve(parent)
		if err != nil {
			return "", err
		}
		return filepath.Join(basePath, child), nil
	}

	return resolve(categoryName)
}

// Import adds the parsed season pack back to qBittorrent with the hash check
// skipped (the matched episodes are already hardlinked into place), rechecks it
// if qBittorrent reports missing files, then resumes it unless the client is
// configured to leave it paused. The returned report identifies how long each
// client operation took.
func (q *qbitClient) Import(req ImportRequest) (ImportReport, error) {
	var report ImportReport
	started := time.Now()
	opts, err := q.buildTorrentAddOptions()
	if err != nil {
		report.record(ImportStageConfig, started)
		return report, err
	}

	resolvedSavePath := strings.TrimSpace(req.SavePath)
	if resolvedSavePath == "" {
		report.record(ImportStageConfig, started)
		return report, importErr(ImportStageConfig, errors.New("resolved qbittorrent save path is empty"))
	}

	// Category-only policy leaves path selection and Auto TMM to qBittorrent.
	// An explicit save or download path opts into a pinned destination and the
	// client's normal Auto TMM opt-out behavior.
	if strings.TrimSpace(q.policy.SavePath) != "" || strings.TrimSpace(q.policy.DownloadPath) != "" {
		opts.SavePath = resolvedSavePath
	}

	lookupHash := req.LegacyHash
	if !req.HasV1 {
		lookupHash = req.V2Hash
	}
	if strings.TrimSpace(lookupHash) == "" {
		report.record(ImportStageConfig, started)
		return report, importErr(ImportStageConfig, errors.New("resolved qbittorrent info hash is empty"))
	}

	report.record(ImportStageConfig, started)

	started = time.Now()
	if _, err := q.c.AddTorrentFromMemory(req.TorrentBytes, opts.Prepare()); err != nil {
		report.record(ImportStageAdd, started)
		return report, importErr(ImportStageAdd, errors.Wrap(err, "failed to add torrent to qbittorrent"))
	}
	report.record(ImportStageAdd, started)

	started = time.Now()
	added, err := q.waitForTorrent(lookupHash)
	report.record(ImportStageFind, started)
	if err != nil {
		return report, importErr(ImportStageFind, err)
	}

	if added.State == qbittorrent.TorrentStateMissingFiles {
		started = time.Now()
		if err := q.c.Recheck([]string{added.Hash}); err != nil {
			report.record(ImportStageRecheck, started)
			return report, importErr(ImportStageRecheck, errors.Wrap(err, "failed to recheck torrent"))
		}

		added, err = q.waitForRecheck(added.Hash)
		report.record(ImportStageRecheck, started)
		if err != nil {
			return report, importErr(ImportStageRecheck, err)
		}
	}

	// a correctly imported torrent always starts once its data is accounted for
	if isActiveTorrentState(added.State) {
		return report, nil
	}

	started = time.Now()
	if err := q.c.Resume([]string{added.Hash}); err != nil {
		report.record(ImportStageResume, started)
		return report, importErr(ImportStageResume, errors.Wrap(err, "failed to resume torrent"))
	}
	report.record(ImportStageResume, started)

	return report, nil
}

// buildTorrentAddOptions maps the client's import policy onto qBittorrent add
// options. The torrent is always added paused with the hash check skipped so it
// can be rechecked before it starts; unset overrides are omitted so qBittorrent
// keeps its own defaults.
func (q *qbitClient) buildTorrentAddOptions() (*qbittorrent.TorrentAddOptions, error) {
	contentLayout, hasLayout, err := resolveContentLayout(q.policy.ContentLayout)
	if err != nil {
		return nil, importErr(ImportStageConfig, err)
	}

	opts := &qbittorrent.TorrentAddOptions{
		SkipHashCheck: true,
		Paused:        true,
	}

	if q.policy.Category != "" {
		opts.Category = strings.TrimSpace(q.policy.Category)
	}
	if q.policy.SavePath != "" {
		opts.SavePath = strings.TrimSpace(q.policy.SavePath)
	}
	if q.policy.DownloadPath != "" {
		opts.DownloadPath = strings.TrimSpace(q.policy.DownloadPath)
	}
	if hasLayout {
		opts.ContentLayout = contentLayout
	}

	if len(q.policy.Tags) > 0 {
		trimmed := make([]string, 0, len(q.policy.Tags))
		for _, tag := range q.policy.Tags {
			if t := strings.TrimSpace(tag); t != "" {
				trimmed = append(trimmed, t)
			}
		}
		if len(trimmed) > 0 {
			opts.Tags = strings.Join(trimmed, ",")
		}
	}

	return opts, nil
}

func resolveContentLayout(mode string) (qbittorrent.ContentLayout, bool, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "":
		return "", false, nil
	case "subfolder":
		return qbittorrent.ContentLayoutSubfolderCreate, true, nil
	case "nosubfolder":
		return qbittorrent.ContentLayoutSubfolderNone, true, nil
	case "original":
		return qbittorrent.ContentLayoutOriginal, true, nil
	default:
		return "", false, errors.New("unsupported content layout %q", mode)
	}
}

// waitForTorrent waits for the added torrent to appear and settle out of the
// transient checking/allocating states. This matters: after a paused skip-check
// add, qBittorrent briefly reports checkingResumeData (with a misleading 100%
// progress) before flipping to missingFiles when data is actually missing.
// Returning on first appearance would miss that and resume into an errored
// torrent, so we wait for the state to stabilise before the caller inspects it.
func (q *qbitClient) waitForTorrent(hash string) (qbittorrent.Torrent, error) {
	var settled qbittorrent.Torrent
	err := pollUntil(q.pollInterval, q.findTimeout, func() (bool, error) {
		t, ok, err := q.lookupTorrent(hash)
		if err != nil {
			return false, err
		}
		if !ok || isCheckingState(t.State) {
			return false, nil
		}
		settled = t
		return true, nil
	})
	if err != nil {
		return qbittorrent.Torrent{}, err
	}
	return settled, nil
}

func (q *qbitClient) waitForRecheck(hash string) (qbittorrent.Torrent, error) {
	var settled qbittorrent.Torrent
	err := pollUntil(q.pollInterval, q.recheckTimeout, func() (bool, error) {
		t, ok, err := q.lookupTorrent(hash)
		if err != nil {
			return false, err
		}
		if !ok || isCheckingState(t.State) || t.State == qbittorrent.TorrentStateMissingFiles {
			return false, nil
		}
		settled = t
		return true, nil
	})
	if err != nil {
		return qbittorrent.Torrent{}, err
	}
	return settled, nil
}

// isCheckingState reports whether the torrent is still in a transient
// checking/allocating/moving state that hasn't stabilised yet.
func isCheckingState(state qbittorrent.TorrentState) bool {
	switch state {
	case qbittorrent.TorrentStateCheckingDl, qbittorrent.TorrentStateCheckingUp,
		qbittorrent.TorrentStateCheckingResumeData, qbittorrent.TorrentStateMoving,
		qbittorrent.TorrentStateAllocating:
		return true
	default:
		return false
	}
}

func (q *qbitClient) lookupTorrent(hash string) (qbittorrent.Torrent, bool, error) {
	torrents, err := q.c.GetTorrents(qbittorrent.TorrentFilterOptions{Hashes: []string{hash}})
	if err != nil {
		return qbittorrent.Torrent{}, false, err
	}

	for _, t := range torrents {
		if strings.EqualFold(t.Hash, hash) || strings.EqualFold(t.InfohashV1, hash) || strings.EqualFold(t.InfohashV2, hash) {
			return t, true, nil
		}
	}

	if len(torrents) == 1 {
		return torrents[0], true, nil
	}

	return qbittorrent.Torrent{}, false, nil
}

func isActiveTorrentState(state qbittorrent.TorrentState) bool {
	switch state {
	case qbittorrent.TorrentStateDownloading, qbittorrent.TorrentStateUploading,
		qbittorrent.TorrentStateStalledDl, qbittorrent.TorrentStateStalledUp,
		qbittorrent.TorrentStateForcedDl, qbittorrent.TorrentStateForcedUp,
		qbittorrent.TorrentStateQueuedDl, qbittorrent.TorrentStateQueuedUp,
		qbittorrent.TorrentStateMetaDl:
		return true
	default:
		return false
	}
}
