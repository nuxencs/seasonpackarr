// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/errtrace"

	"github.com/Masterminds/semver"
	"github.com/autobrr/go-qbittorrent"
)

const (
	qbitFileReadWorkers = 4
	// qbitInventoryReadWorkers limits the parallel torrents/info reads of a scope with several include values.
	qbitInventoryReadWorkers = 4
)

// qbitAPI is the subset of *qbittorrent.Client the adapter uses. It exists so
// the import machinery can be unit-tested against a fake without a live client.
type qbitAPI interface {
	GetTorrents(o qbittorrent.TorrentFilterOptions) ([]qbittorrent.Torrent, error)
	GetFilesInformation(hash string) (*qbittorrent.TorrentFiles, error)
	AddTorrentFromMemory(buf []byte, options map[string]string) (*qbittorrent.TorrentAddResponse, error)
	GetCategories() (map[string]qbittorrent.Category, error)
	GetDefaultSavePath() (string, error)
	GetAppPreferences() (qbittorrent.AppPreferences, error)
	Recheck(hashes []string) error
	Stop(hashes []string) error
	Resume(hashes []string) error
	RequiresMinVersion(minVersion *semver.Version) (bool, error)
}

// qbitTagFilterVersion is the WebAPI version of qBittorrent 4.4.0. Older
// versions ignore the tag filter of torrents/info and return every torrent.
var qbitTagFilterVersion = semver.MustParse("2.8.4")

type qbitClient struct {
	c      qbitAPI
	policy domain.ImportPolicy
	// scope limits GetTorrents only. The lookup of the added torrent ignores it.
	scope domain.InventoryScope
	// inventoryReadWorkers overrides qbitInventoryReadWorkers, so benchmarks can compare serial reads.
	inventoryReadWorkers int

	// timeouts are fields so tests can shrink them to milliseconds.
	findTimeout  time.Duration
	pollInterval time.Duration
}

func newQbitClient(ctx context.Context, client *domain.Client) (*qbitClient, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	host, err := buildHost(client)
	if err != nil {
		return nil, fmt.Errorf("failed to build host: %w", err)
	}

	c := qbittorrent.NewClient(qbittorrent.Config{
		Host:     host,
		Username: client.Username,
		Password: client.Password,
		APIKey:   client.APIKey,
	})
	api := safeQbitAPI{api: c, redact: qbitAddressRedactor(host)}

	if err := c.Login(); err != nil {
		return nil, errtrace.WithStack(fmt.Errorf("failed to login to qbittorrent: %w", api.safe(err)))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return &qbitClient{
		c:            api,
		policy:       client.Import,
		scope:        client.Inventory,
		findTimeout:  15 * time.Second,
		pollInterval: 250 * time.Millisecond,
	}, nil
}

// qbitAddressRedactor returns a function that hides what follows the host in
// each URL of the client address: the path, which is the key of a qui proxy
// address, the user info, and the query. The request path stays for diagnosis,
// without its query. It returns nil when the address has no such part.
func qbitAddressRedactor(host string) func(string) string {
	u, err := url.Parse(host)
	if err != nil || (strings.Trim(u.Path, "/") == "" && u.User == nil && u.RawQuery == "" && u.Fragment == "") {
		return nil
	}
	base := u.Scheme + "://" + u.Host
	// The pattern matches by host, not by the configured text: go-qbittorrent
	// cleans the path, and net/http shows the password as "***".
	address := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(u.Scheme) + `://(?:[^/\s"]*@)?` + regexp.QuoteMeta(u.Host) + `[^\s"]*`)
	return func(text string) string {
		return address.ReplaceAllStringFunc(text, func(found string) string {
			if _, request, ok := strings.Cut(found, "/api/v2/"); ok {
				if end := strings.IndexAny(request, "?#"); end >= 0 {
					request = request[:end]
				}
				return base + "/[redacted]/api/v2/" + request
			}
			return base + "/[redacted]"
		})
	}
}

// safeQbitAPI removes the secret parts of the client address from every error
// of the wrapped client. go-qbittorrent puts the request URL into its error
// text, and the adapter errors go to the log, the API response, and the
// notification.
type safeQbitAPI struct {
	api    qbitAPI
	redact func(string) string
}

// safe returns the text only when it hides something, so that no wrapped error
// can show the address.
func (s safeQbitAPI) safe(err error) error {
	if err == nil || s.redact == nil {
		return err
	}
	text := err.Error()
	if safe := s.redact(text); safe != text {
		return errors.New(safe)
	}
	return err
}

func (s safeQbitAPI) GetTorrents(o qbittorrent.TorrentFilterOptions) ([]qbittorrent.Torrent, error) {
	torrents, err := s.api.GetTorrents(o)
	return torrents, s.safe(err)
}

func (s safeQbitAPI) GetFilesInformation(hash string) (*qbittorrent.TorrentFiles, error) {
	files, err := s.api.GetFilesInformation(hash)
	return files, s.safe(err)
}

func (s safeQbitAPI) AddTorrentFromMemory(buf []byte, options map[string]string) (*qbittorrent.TorrentAddResponse, error) {
	response, err := s.api.AddTorrentFromMemory(buf, options)
	return response, s.safe(err)
}

func (s safeQbitAPI) GetCategories() (map[string]qbittorrent.Category, error) {
	categories, err := s.api.GetCategories()
	return categories, s.safe(err)
}

func (s safeQbitAPI) GetDefaultSavePath() (string, error) {
	path, err := s.api.GetDefaultSavePath()
	return path, s.safe(err)
}

func (s safeQbitAPI) GetAppPreferences() (qbittorrent.AppPreferences, error) {
	preferences, err := s.api.GetAppPreferences()
	return preferences, s.safe(err)
}

func (s safeQbitAPI) Recheck(hashes []string) error { return s.safe(s.api.Recheck(hashes)) }

func (s safeQbitAPI) Stop(hashes []string) error { return s.safe(s.api.Stop(hashes)) }

func (s safeQbitAPI) Resume(hashes []string) error { return s.safe(s.api.Resume(hashes)) }

func (s safeQbitAPI) RequiresMinVersion(minVersion *semver.Version) (bool, error) {
	ok, err := s.api.RequiresMinVersion(minVersion)
	return ok, s.safe(err)
}

// GetTorrents reads the inventory. qBittorrent filters the includes, and the
// adapter drops the excludes after the read, because torrents/info has no
// exclude filter. torrents/info takes one category and one tag, and both
// together must match, so each include value is one read. The reads run in
// parallel and merge by hash in a fixed order.
func (q *qbitClient) GetTorrents(ctx context.Context) ([]Torrent, error) {
	if !q.scope.IsSet() {
		return q.readInventory(ctx, []qbittorrent.TorrentFilterOptions{{}})
	}

	filters := make([]qbittorrent.TorrentFilterOptions, 0, len(q.scope.IncludeCategories)+len(q.scope.IncludeTags))
	for _, category := range q.scope.IncludeCategories {
		filters = append(filters, qbittorrent.TorrentFilterOptions{Category: category})
	}
	switch {
	case !q.scope.HasIncludes():
		filters = append(filters, qbittorrent.TorrentFilterOptions{})
	case len(q.scope.IncludeTags) == 0:
	case q.supportsTagFilter():
		for _, tag := range q.scope.IncludeTags {
			filters = append(filters, qbittorrent.TorrentFilterOptions{Tag: tag})
		}
	default:
		// The category reads stay on the server, because its filter includes subcategories.
		filters = append(filters, qbittorrent.TorrentFilterOptions{})
	}
	return q.readInventory(ctx, filters)
}

// supportsTagFilter reports whether torrents/info filters by tag. A failed
// version read also falls back to a local tag filter: the result stays correct, only larger.
func (q *qbitClient) supportsTagFilter() bool {
	supported, _ := q.c.RequiresMinVersion(qbitTagFilterVersion)
	return supported
}

// readInventory sends the reads, keeps each hash once, and drops the torrents
// outside the scope. An unfiltered read in a scope with includes supplies the
// include tags on qBittorrent 4.3.
func (q *qbitClient) readInventory(ctx context.Context, filters []qbittorrent.TorrentFilterOptions) ([]Torrent, error) {
	results, err := q.readFilters(ctx, filters)
	if err != nil {
		return nil, err
	}

	torrents := make([]Torrent, 0, len(results[0]))
	// One read cannot repeat a hash, so only several reads need the merge.
	var seen map[string]bool
	if len(filters) > 1 {
		seen = make(map[string]bool)
	}
	for index, ts := range results {
		unfiltered := filters[index].Category == "" && filters[index].Tag == ""
		for _, t := range ts {
			if q.scope.IsSet() {
				if unfiltered && q.scope.HasIncludes() && !qbitHasTag(t.Tags, q.scope.IncludesTag) {
					continue
				}
				if q.scope.ExcludesCategory(t.Category) || qbitHasTag(t.Tags, q.scope.ExcludesTag) {
					continue
				}
			}
			if seen != nil {
				if seen[t.Hash] {
					continue
				}
				seen[t.Hash] = true
			}
			torrents = append(torrents, Torrent{
				Hash:     t.Hash,
				Name:     t.Name,
				SavePath: t.SavePath,
			})
		}
	}
	return torrents, nil
}

// readFilters sends one torrents/info read for each filter, with at most
// qbitInventoryReadWorkers at a time. The results keep the order of filters,
// and the error of the first failed filter wins.
func (q *qbitClient) readFilters(ctx context.Context, filters []qbittorrent.TorrentFilterOptions) ([][]qbittorrent.Torrent, error) {
	results := make([][]qbittorrent.Torrent, len(filters))
	errs := make([]error, len(filters))
	read := func(index int) {
		if err := ctx.Err(); err != nil {
			errs[index] = err
			return
		}
		ts, err := q.c.GetTorrents(filters[index])
		if err != nil {
			errs[index] = errtrace.WithStack(fmt.Errorf("failed to get torrents: %w", err))
			return
		}
		results[index] = ts
	}

	if len(filters) == 1 {
		read(0)
	} else {
		slots := make(chan struct{}, cmp.Or(q.inventoryReadWorkers, qbitInventoryReadWorkers))
		var wg sync.WaitGroup
		for index := range filters {
			wg.Go(func() {
				select {
				case slots <- struct{}{}:
					defer func() { <-slots }()
					read(index)
				case <-ctx.Done():
					errs[index] = ctx.Err()
				}
			})
		}
		wg.Wait()
	}

	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// qbitHasTag reports whether match accepts one of the tags of a torrent.
// qBittorrent lists tags as "a, b". The loop allocates nothing, because it
// runs once for each torrent of the inventory.
func qbitHasTag(tags string, match func(string) bool) bool {
	for rest := tags; rest != ""; {
		var tag string
		tag, rest, _ = strings.Cut(rest, ",")
		if tag = strings.TrimSpace(tag); tag != "" && match(tag) {
			return true
		}
	}
	return false
}

func (q *qbitClient) GetFiles(ctx context.Context, hashes []string) []FileResult {
	results := make([]FileResult, len(hashes))
	if len(hashes) == 0 {
		return results
	}

	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(qbitFileReadWorkers, len(hashes)) {
		workers.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case index, ok := <-jobs:
					if !ok {
						return
					}
					results[index] = q.getFiles(ctx, hashes[index])
				}
			}
		})
	}

sendJobs:
	for index := range hashes {
		select {
		case jobs <- index:
		case <-ctx.Done():
			break sendJobs
		}
	}
	close(jobs)
	workers.Wait()
	for index, result := range results {
		if result.Hash == "" {
			results[index] = FileResult{Hash: hashes[index], Err: ctx.Err()}
		}
	}
	return results
}

func (q *qbitClient) getFiles(ctx context.Context, hash string) FileResult {
	torrentFiles, err := q.c.GetFilesInformation(hash)
	if err != nil {
		return FileResult{Hash: hash, Err: errtrace.WithStack(fmt.Errorf("failed to get files: %w", err))}
	}
	if err := ctx.Err(); err != nil {
		return FileResult{Hash: hash, Err: err}
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
func (q *qbitClient) ImportDestination(ctx context.Context) (ImportDestination, error) {
	if err := ctx.Err(); err != nil {
		return ImportDestination{}, err
	}
	categoryOnly := strings.TrimSpace(q.policy.SavePath) == "" && strings.TrimSpace(q.policy.DownloadPath) == ""
	contentLayout := strings.TrimSpace(q.policy.ContentLayout)

	var preferences qbittorrent.AppPreferences
	if categoryOnly || contentLayout == "" {
		var err error
		preferences, err = q.c.GetAppPreferences()
		if err != nil {
			return ImportDestination{}, importErr(ImportStageConfig, errtrace.WithStack(fmt.Errorf("could not read qbittorrent preferences: %w", err)))
		}
		if err := ctx.Err(); err != nil {
			return ImportDestination{}, err
		}
	}

	var actualPath string
	if q.policy.SavePath != "" {
		actualPath = q.policy.SavePath
	} else {
		categories, err := q.c.GetCategories()
		if err != nil {
			return ImportDestination{}, importErr(ImportStageConfig, errtrace.WithStack(fmt.Errorf("could not read qbittorrent categories: %w", err)))
		}
		if err := ctx.Err(); err != nil {
			return ImportDestination{}, err
		}

		_, ok := categories[q.policy.Category]
		if !ok {
			return ImportDestination{}, importErr(ImportStageConfig, fmt.Errorf("qbit category %q was not found in qbittorrent", q.policy.Category))
		}

		useCategoryPath := !categoryOnly || preferences.AutoTmmEnabled || preferences.UseCategoryPathsInManualMode
		if !useCategoryPath {
			defaultSavePath, err := q.c.GetDefaultSavePath()
			if err != nil {
				return ImportDestination{}, importErr(ImportStageConfig, errtrace.WithStack(fmt.Errorf("could not read qbittorrent default save path: %w", err)))
			}
			if err := ctx.Err(); err != nil {
				return ImportDestination{}, err
			}
			actualPath = defaultSavePath
		} else {
			var err error
			actualPath, err = q.resolveCategorySavePath(ctx, q.policy.Category, categories)
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

func (q *qbitClient) resolveCategorySavePath(ctx context.Context, categoryName string, categories map[string]qbittorrent.Category) (string, error) {
	defaultSavePath := func() (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		path, err := q.c.GetDefaultSavePath()
		if err != nil {
			return "", importErr(ImportStageConfig, errtrace.WithStack(fmt.Errorf("could not read qbittorrent default save path: %w", err)))
		}
		if err := ctx.Err(); err != nil {
			return "", err
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

// Import adds the parsed season pack to qBittorrent and returns without waiting
// for a hash check. A complete pack is added stopped with the hash check
// skipped, then started once qBittorrent confirms its files. A partial pack is
// added started with a normal check, so qBittorrent checks the hardlinked data
// and then downloads the missing pieces by itself. The returned report
// identifies how long each client operation took.
func (q *qbitClient) Import(ctx context.Context, req ImportRequest) (ImportReport, error) {
	var report ImportReport
	started := time.Now()
	if err := ctx.Err(); err != nil {
		return report, err
	}
	opts, err := q.buildTorrentAddOptions(req.DataComplete)
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
		opts["savepath"] = resolvedSavePath
		opts["autoTMM"] = "false"
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
	if _, err := q.c.AddTorrentFromMemory(req.TorrentBytes, opts); err != nil {
		report.record(ImportStageAdd, started)
		return report, importErr(ImportStageAdd, fmt.Errorf("failed to add torrent to qbittorrent: %w", err))
	}
	report.record(ImportStageAdd, started)

	started = time.Now()
	// only the stopped skip-check add of a complete pack must settle
	added, err := q.waitForTorrent(ctx, lookupHash, req.DataComplete)
	report.record(ImportStageFind, started)
	if err != nil {
		return report, importErr(ImportStageFind, err)
	}

	// a started add can still be checking; qBittorrent starts it after the check
	if isActiveTorrentState(added.State) || isCheckingState(added.State) {
		return report, nil
	}

	// Only a complete skip-check add can report missingFiles, when a file changed
	// after the processor checked it. A recheck on a stopped torrent sets the
	// FilesChecked stop condition, and only stop clears it. Recheck, stop, then
	// start lets qBittorrent check and start the torrent without a wait here.
	if added.State == qbittorrent.TorrentStateMissingFiles {
		started = time.Now()
		if err := q.c.Recheck([]string{added.Hash}); err != nil {
			report.record(ImportStageRecheck, started)
			return report, importErr(ImportStageRecheck, fmt.Errorf("failed to recheck torrent: %w", err))
		}
		if err := q.c.Stop([]string{added.Hash}); err != nil {
			report.record(ImportStageRecheck, started)
			return report, importErr(ImportStageRecheck, fmt.Errorf("failed to clear recheck stop condition: %w", err))
		}
		report.record(ImportStageRecheck, started)
	}

	started = time.Now()
	if err := q.c.Resume([]string{added.Hash}); err != nil {
		report.record(ImportStageResume, started)
		return report, importErr(ImportStageResume, fmt.Errorf("failed to resume torrent: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return report, importErr(ImportStageResume, err)
	}
	report.record(ImportStageResume, started)

	return report, nil
}

// buildTorrentAddOptions maps the client's import policy onto qBittorrent add
// parameters. A complete pack is added stopped with the hash check skipped. A
// partial pack is added started with a normal check and no stop condition, so
// global "add stopped" preferences cannot leave it stopped after the check.
// Unset overrides are omitted so qBittorrent keeps its own defaults.
func (q *qbitClient) buildTorrentAddOptions(dataComplete bool) (map[string]string, error) {
	contentLayout, hasLayout, err := resolveContentLayout(q.policy.ContentLayout)
	if err != nil {
		return nil, importErr(ImportStageConfig, err)
	}

	opts := &qbittorrent.TorrentAddOptions{
		SkipHashCheck: dataComplete,
		Paused:        dataComplete,
	}

	if q.policy.Category != "" {
		opts.Category = strings.TrimSpace(q.policy.Category)
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

	params := opts.Prepare()
	if !dataComplete {
		// qBittorrent 4.5+ applies its global stop condition when this is unset.
		params["stopCondition"] = "None"
	}
	return params, nil
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
		return "", false, fmt.Errorf("unsupported content layout %q", mode)
	}
}

// waitForTorrent waits for the added torrent to appear. With settle set, it also
// waits for the state to leave the transient checking states. This matters for
// a stopped skip-check add: qBittorrent briefly reports checkingResumeData (with
// a misleading 100% progress) before flipping to missingFiles when data is
// actually missing, and returning on first appearance would miss that. A
// started add with a normal check must not settle, because its check can take
// longer than the caller's request timeout.
func (q *qbitClient) waitForTorrent(ctx context.Context, hash string, settle bool) (qbittorrent.Torrent, error) {
	ctx, cancel := context.WithTimeout(ctx, q.findTimeout)
	defer cancel()

	var found qbittorrent.Torrent
	err := pollUntil(ctx, q.pollInterval, func() (bool, error) {
		t, ok, err := q.lookupTorrent(ctx, hash)
		if err != nil {
			return false, err
		}
		if !ok || (settle && isCheckingState(t.State)) {
			return false, nil
		}
		found = t
		return true, nil
	})
	if err != nil {
		return qbittorrent.Torrent{}, err
	}
	return found, nil
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

func (q *qbitClient) lookupTorrent(ctx context.Context, hash string) (qbittorrent.Torrent, bool, error) {
	if err := ctx.Err(); err != nil {
		return qbittorrent.Torrent{}, false, err
	}
	torrents, err := q.c.GetTorrents(qbittorrent.TorrentFilterOptions{Hashes: []string{hash}})
	if err != nil {
		return qbittorrent.Torrent{}, false, err
	}
	if err := ctx.Err(); err != nil {
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
