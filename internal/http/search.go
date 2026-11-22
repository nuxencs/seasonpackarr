// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/errtrace"
	"github.com/nuxencs/seasonpackarr/internal/format"
	"github.com/nuxencs/seasonpackarr/internal/inventory"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/prowlarr"
	"github.com/nuxencs/seasonpackarr/internal/release"
	"github.com/nuxencs/seasonpackarr/internal/seasonpack"
	"github.com/nuxencs/seasonpackarr/internal/state"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/autobrr/rls"
	"github.com/gin-gonic/gin"
)

var (
	errSearchRunning = errors.New("a Prowlarr discovery run is already active")
	errDatabase      = errors.New("could not access database; check service logs")
)

// SearchRequest selects one configured client or all clients when empty.
type SearchRequest struct {
	ClientName string `json:"clientname"`
	DryRun     bool   `json:"dryRun"`
	Verify     bool   `json:"verify"`
}

type searchOutcome struct {
	ClientName       string `json:"clientname"`
	Title            string `json:"title"`
	IndexerID        int    `json:"indexerId"`
	Status           string `json:"status"`
	Reason           string `json:"reason,omitempty"`
	ReusableEpisodes *int   `json:"reusableEpisodes"`
	TotalEpisodes    *int   `json:"totalEpisodes"`
}

type searchFailure struct {
	ClientName string `json:"clientname,omitempty"`
	Query      string `json:"query,omitempty"`
	IndexerID  int    `json:"indexerId,omitzero"`
	Reason     string `json:"reason"`
}

type searchReport struct {
	RSS                    bool            `json:"-"`
	Verify                 bool            `json:"verify"`
	TorrentDownloads       int             `json:"torrentDownloads"`
	TorrentCacheHits       int             `json:"torrentCacheHits"`
	DryRun                 bool            `json:"dryRun"`
	ScannedTorrents        int             `json:"scannedTorrents"`
	EpisodeTorrents        int             `json:"episodeTorrents"`
	CoveredEpisodeTorrents int             `json:"coveredEpisodeTorrents"`
	Groups                 int             `json:"groups"`
	Requests               int             `json:"requests"`
	Outcomes               []searchOutcome `json:"outcomes"`
	Failures               []searchFailure `json:"failures"`
}

type searchRunner struct {
	cfg         config.Provider
	inv         *inventory.Store
	log         logger.Logger
	processor   *seasonpack.Processor
	noti        *notifier
	running     atomic.Bool
	cooldowns   map[int]time.Time
	state       *state.Store
	provider    *prowlarr.Client
	prowlarrURL string
	prowlarrKey string
}

// discoveryRun holds state shared by one manual search or RSS poll. The runner
// retains the connection. Durable state is loaded from SQLite for each run.
type discoveryRun struct {
	runner  *searchRunner
	request SearchRequest
	report  *searchReport
	// clients holds one handle for each client that the run could read. Each
	// handle has the config snapshot of the run, so a config reload during the
	// run changes no matching rule, no import rule, and no adapter.
	clients       map[string]*inventory.Client
	selected      map[string][]rls.Release
	unavailable   map[int]bool
	storageFailed bool
}

func (r *searchRunner) handler(c *gin.Context) {
	var req *SearchRequest
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || req == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid search request"})
		return
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "search request must contain one JSON object"})
		return
	}
	report, err := r.run(c.Request.Context(), *req)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errSearchRunning) {
			status = http.StatusConflict
		} else if errors.Is(err, errDatabase) {
			status = http.StatusInternalServerError
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, report)
}

func (r *searchRunner) run(ctx context.Context, req SearchRequest) (searchReport, error) {
	return r.discover(ctx, req, false)
}

// poll is the scheduler-only entrypoint. RSS always evaluates imports for all clients.
func (r *searchRunner) poll(ctx context.Context) (searchReport, error) {
	return r.discover(ctx, SearchRequest{}, true)
}

func (r *searchRunner) discover(ctx context.Context, req SearchRequest, rss bool) (searchReport, error) {
	report := searchReport{DryRun: req.DryRun, Verify: req.Verify, RSS: rss, Outcomes: []searchOutcome{}, Failures: []searchFailure{}}
	if req.Verify && !req.DryRun {
		return report, errors.New("verify requires dryRun")
	}
	if !r.running.CompareAndSwap(false, true) {
		return report, errSearchRunning
	}
	defer r.running.Store(false)
	snapshot := r.cfg.Snapshot()
	spacing, err := time.ParseDuration(snapshot.Search.RequestInterval)
	if err != nil || spacing < 0 {
		return report, errors.New("invalid search.requestInterval")
	}
	if r.provider == nil || r.prowlarrURL != snapshot.Search.ProwlarrURL || r.prowlarrKey != snapshot.Search.APIKey {
		provider, err := prowlarr.New(snapshot.Search.ProwlarrURL, snapshot.Search.APIKey, spacing, nil)
		if err != nil {
			return report, err
		}
		r.provider = provider
		r.prowlarrURL = snapshot.Search.ProwlarrURL
		r.prowlarrKey = snapshot.Search.APIKey
	}
	provider := r.provider
	provider.SetRequestInterval(spacing)
	if err := r.state.UseConnection(ctx, snapshot.Search.ProwlarrURL, snapshot.Search.APIKey); err != nil {
		return report, r.stateError(err)
	}
	// New failures remain in this working set even when Retry-After has elapsed.
	r.cooldowns, err = r.state.Cooldowns(ctx, time.Now())
	if err != nil {
		return report, r.stateError(err)
	}
	clients := slices.Sorted(maps.Keys(snapshot.Clients))
	if req.ClientName != "" {
		if _, ok := snapshot.Clients[req.ClientName]; !ok {
			return report, errors.New("search client is not configured")
		}
		clients = []string{req.ClientName}
	}
	started := time.Now()
	defer func() {
		for _, failure := range report.Failures {
			r.log.Warn().Bool("rss", rss).Str("clientname", failure.ClientName).Str("query", failure.Query).Int("indexer_id", failure.IndexerID).Str("reason", failure.Reason).Msg("Prowlarr operation failed")
		}
		for _, outcome := range report.Outcomes {
			r.log.Info().Bool("rss", rss).Str("clientname", outcome.ClientName).Str("release", outcome.Title).Int("indexer_id", outcome.IndexerID).Str("status", outcome.Status).Str("reason", outcome.Reason).Interface("reusable_episodes", outcome.ReusableEpisodes).Interface("total_episodes", outcome.TotalEpisodes).Msg("Prowlarr result")
		}
		r.log.Info().Bool("rss", rss).Bool("dry_run", req.DryRun).Bool("verify", req.Verify).Int("torrent_downloads", report.TorrentDownloads).Int("torrent_cache_hits", report.TorrentCacheHits).Int("covered_episode_torrents", report.CoveredEpisodeTorrents).Int("groups", report.Groups).Int("search_requests", report.Requests).Int("outcomes", len(report.Outcomes)).Int("failures", len(report.Failures)).Int64("duration_ms", time.Since(started).Milliseconds()).Msg("Prowlarr run completed")
	}()

	handles := make(map[string]*inventory.Client, len(clients))
	groups := make(map[seasonSearchKey]*seasonSearch)
	for _, name := range clients {
		if ctx.Err() != nil {
			break
		}
		client, err := r.inv.Client(ctx, name, snapshot)
		if err != nil {
			report.Failures = append(report.Failures, searchFailure{ClientName: name, Reason: "could not connect to torrent client"})
			continue
		}
		// Search starts from fresh summaries. Exact planning can reuse this scan.
		r.inv.DiscardClient(name)
		clientInventory, err := client.Inventory(ctx)
		if err != nil {
			report.Failures = append(report.Failures, searchFailure{ClientName: name, Reason: "could not read torrent client inventory"})
			continue
		}
		handles[name] = client
		for entries := range clientInventory.TitleGroups() {
			// Inventory buckets already apply the title, season, and year rules.
			// Collect packs once so episode-only seasons do not need pairwise scans.
			var packs []*rls.Release
			for entry := range entries {
				if entry.Release.Type.Is(rls.Series) && entry.Release.Episode == 0 {
					packs = append(packs, entry.Release)
				}
			}
			for entry := range entries {
				report.ScannedTorrents++
				parsed := entry.Release
				// Date-based and ambiguous releases need metadata that this feature does
				// not have. Existing season packs never create search groups.
				if parsed.Type != rls.Episode || parsed.Series <= 0 || parsed.Episode <= 0 || strings.TrimSpace(parsed.Title) == "" {
					continue
				}
				report.EpisodeTorrents++
				if slices.ContainsFunc(packs, func(pack *rls.Release) bool {
					return release.CheckCandidates(*pack, *parsed, snapshot.FuzzyMatching).StatusCode == domain.StatusSuccessfulMatch
				}) {
					report.CoveredEpisodeTorrents++
					continue
				}
				// Years affect compatibility, but never the outgoing query identity.
				key := seasonSearchKey{Title: rls.MustNormalize(parsed.Title), Season: parsed.Series}
				group := groups[key]
				query := prowlarr.Query{Title: parsed.Title, Season: key.Season}
				if group == nil {
					group = &seasonSearch{query: query, clients: make(map[string]bool)}
					groups[key] = group
				}
				// Stable spelling independent of map and client inventory order.
				if query.Title < group.query.Title {
					group.query.Title = query.Title
				}
				group.clients[name] = true
			}
		}
	}
	ordered := slices.Collect(maps.Values(groups))
	slices.SortFunc(ordered, func(a, b *seasonSearch) int { return strings.Compare(a.query.String(), b.query.String()) })
	report.Groups = len(ordered)
	if len(ordered) == 0 {
		if ctx.Err() != nil {
			report.Failures = append(report.Failures, searchFailure{Reason: ctx.Err().Error()})
		}
		return report, nil
	}
	if until := r.cooldowns[state.ProwlarrCooldownID]; time.Now().Before(until) {
		report.Failures = append(report.Failures, searchFailure{Reason: "Prowlarr cooldown is still active"})
		return report, nil
	}
	indexers, err := provider.Indexers(ctx)
	if err != nil {
		if stateErr := r.recordCooldown(ctx, state.ProwlarrCooldownID, err); stateErr != nil {
			report.Failures = append(report.Failures, searchFailure{Reason: r.stateError(stateErr).Error()})
		}
		report.Failures = append(report.Failures, searchFailure{Reason: err.Error()})
		return report, nil
	}
	capability := "searchable"
	if rss {
		capability = "RSS-capable"
	}
	indexers = slices.DeleteFunc(indexers, func(indexer prowlarr.Indexer) bool {
		if rss {
			return !indexer.SupportsRSS
		}
		return !indexer.SupportsSearch
	})
	if len(snapshot.Search.IndexerIDs) > 0 {
		for _, id := range snapshot.Search.IndexerIDs {
			if !slices.ContainsFunc(indexers, func(indexer prowlarr.Indexer) bool { return indexer.ID == id }) {
				report.Failures = append(report.Failures, searchFailure{IndexerID: id, Reason: "selected indexer is missing, disabled, or not a " + capability + " torrent indexer"})
			}
		}
		indexers = slices.DeleteFunc(indexers, func(indexer prowlarr.Indexer) bool { return !slices.Contains(snapshot.Search.IndexerIDs, indexer.ID) })
	}
	if len(indexers) == 0 {
		report.Failures = append(report.Failures, searchFailure{Reason: "no enabled " + capability + " torrent indexers in Prowlarr"})
		return report, nil
	}
	run := discoveryRun{
		runner:      r,
		request:     req,
		report:      &report,
		clients:     handles,
		selected:    make(map[string][]rls.Release),
		unavailable: make(map[int]bool),
	}
	for _, indexer := range indexers {
		if until := r.cooldowns[indexer.ID]; time.Now().Before(until) {
			run.unavailable[indexer.ID] = true
			report.Failures = append(report.Failures, searchFailure{IndexerID: indexer.ID, Reason: "indexer cooldown or Retry-After deadline has not elapsed"})
		}
	}
	if rss {
		feeds, err := r.state.Feeds(ctx)
		if err != nil {
			run.storageFailure(err)
			return report, nil
		}
		// Do not consume releases for clients absent from this inventory scan.
		persist := len(handles) == len(clients)
		feeds.Prune(indexers, time.Now())
		if !run.saveFeeds(ctx, feeds, persist) {
			return report, nil
		}
		for _, indexer := range indexers {
			if ctx.Err() != nil || run.storageFailed {
				break
			}
			if !run.unavailable[indexer.ID] {
				run.pollRSS(ctx, indexer, groups, feeds, persist)
			}
		}
	} else {
		for _, group := range ordered {
			for _, indexer := range indexers {
				if ctx.Err() != nil || run.storageFailed {
					break
				}
				if run.unavailable[indexer.ID] {
					continue
				}
				run.searchGroup(ctx, indexer, group)
			}
			if ctx.Err() != nil || run.storageFailed {
				break
			}
		}
	}
	if ctx.Err() != nil {
		report.Failures = append(report.Failures, searchFailure{Reason: ctx.Err().Error()})
	}
	return report, nil
}

type (
	seasonSearchKey struct {
		Title  string
		Season int
	}
	seasonSearch struct {
		query   prowlarr.Query
		clients map[string]bool
	}
)

func (run *discoveryRun) searchGroup(ctx context.Context, indexer prowlarr.Indexer, group *seasonSearch) {
	seen := make(map[string]bool)
	offset := 0
	const maxPages = 10
	for range maxPages {
		if ctx.Err() != nil || run.storageFailed {
			return
		}
		run.report.Requests++
		results, limit, err := run.runner.provider.SearchPage(ctx, indexer, group.query, offset)
		if err != nil {
			run.report.Failures = append(run.report.Failures, searchFailure{Query: group.query.String(), IndexerID: indexer.ID, Reason: err.Error()})
			// Do not hammer an unavailable or rate-limited indexer for every season.
			run.unavailable[indexer.ID] = true
			if stateErr := run.runner.recordCooldown(ctx, indexer.ID, err); stateErr != nil {
				run.storageFailure(stateErr)
			}
			return
		}
		fresh := 0
		for _, result := range results {
			if ctx.Err() != nil || run.storageFailed {
				return
			}
			id := cmp.Or(result.GUID, result.Link, result.Enclosure.URL, result.Title)
			if seen[id] {
				continue
			}
			seen[id] = true
			fresh++
			run.evaluateResult(ctx, indexer, result, group)
			if _, failed := run.runner.cooldowns[indexer.ID]; failed {
				run.unavailable[indexer.ID] = true
				return
			}
		}
		if !indexer.SupportsPagination || len(results) < limit {
			return
		}
		if fresh == 0 {
			run.report.Failures = append(run.report.Failures, searchFailure{Query: group.query.String(), IndexerID: indexer.ID, Reason: "indexer repeated a page; remaining results may be incomplete"})
			return
		}
		offset += len(results)
	}
	run.report.Failures = append(run.report.Failures, searchFailure{Query: group.query.String(), IndexerID: indexer.ID, Reason: "search reached the 10-page limit; remaining results may be incomplete"})
}

func (run *discoveryRun) evaluateResult(ctx context.Context, indexer prowlarr.Indexer, result prowlarr.Result, group *seasonSearch) {
	processor := run.runner.processor
	torrent := resultTorrent{run: run, indexer: indexer, result: result}
	for _, name := range slices.Sorted(maps.Keys(group.clients)) {
		if ctx.Err() != nil || run.storageFailed {
			return
		}
		client := run.clients[name]
		snapshot := client.Snapshot()
		parsed := rls.ParseString(result.Title)
		outcome := searchOutcome{ClientName: name, Title: result.Title, IndexerID: indexer.ID, Status: "rejected"}
		duplicate := false
		for selectedClient, acceptedReleases := range run.selected {
			if torrentclient.EndpointOf(*snapshot.Clients[name]) != torrentclient.EndpointOf(*snapshot.Clients[selectedClient]) {
				continue
			}
			for _, accepted := range acceptedReleases {
				if format.ComparableTitle(parsed, snapshot.FuzzyMatching) == format.ComparableTitle(accepted, snapshot.FuzzyMatching) && release.CheckCandidates(parsed, accepted, snapshot.FuzzyMatching).StatusCode == domain.StatusAlreadyInClient {
					duplicate = true
					break
				}
			}
			if duplicate {
				break
			}
		}
		if duplicate {
			outcome.Reason = "release variant already selected in this run"
			run.report.Outcomes = append(run.report.Outcomes, outcome)
			continue
		}
		if run.request.DryRun && !run.request.Verify {
			if checked := processor.Candidate(ctx, client, result.Title); checked.Err != nil {
				outcome.reportError(checked)
			} else {
				outcome.Status = "candidate"
				outcome.Reason = "release checks passed; exact coverage not verified"
			}
			run.report.Outcomes = append(run.report.Outcomes, outcome)
			continue
		}
		matched := processor.Match(ctx, client, result.Title, seasonpack.TorrentFetch(torrent.fetch))
		if torrent.storageErr != nil {
			run.storageFailure(torrent.storageErr)
			return
		}
		if matched.Coverage != nil {
			outcome.ReusableEpisodes = new(matched.Coverage.Reusable)
			outcome.TotalEpisodes = new(matched.Coverage.Total)
		}
		if matched.Err != nil {
			switch {
			case errors.Is(matched.Err, seasonpack.ErrFileDetailRead):
				outcome.Status = "failed"
				outcome.Reason = "could not read episode file details from torrent client"
			case errors.Is(matched.Err, seasonpack.ErrNoSourceOnDisk):
				outcome.Reason = "no accessible episode files with the declared size"
			case torrent.downloadErr != nil && errors.Is(matched.Err, torrent.downloadErr):
				if err := run.runner.recordCooldown(ctx, indexer.ID, torrent.downloadErr); err != nil {
					run.storageFailure(err)
				}
				outcome.Status = "failed"
				outcome.Reason = torrent.downloadErr.Error()
			default:
				outcome.reportError(matched)
			}
			run.report.Outcomes = append(run.report.Outcomes, outcome)
			continue
		}
		if run.request.DryRun {
			outcome.Status = "would_import"
			run.selected[name] = append(run.selected[name], parsed)
		} else {
			// The import takes the plan of the match from the processor, as for
			// the two requests of an announce.
			imported := processor.Import(ctx, client, result.Title, torrent.data)
			if imported.Err != nil {
				outcome.reportError(imported)
			} else {
				outcome.Status = "imported"
			}
			// After an import attempt, a client may already contain the pack even if
			// verification/resume failed. Leave recovery to the next inventory scan.
			run.selected[name] = append(run.selected[name], parsed)
			action := "Search"
			if run.report.RSS {
				action = "RSS"
			}
			log := run.runner.log.With().Str("module", "processor").Str("release", result.Title).Str("clientname", name).Logger()
			run.runner.noti.send(log, imported.Status, domain.NotificationPayload{
				ReleaseName: result.Title,
				Client:      name,
				Action:      action,
				Error:       imported.Err,
			})
		}
		run.report.Outcomes = append(run.report.Outcomes, outcome)
	}
}

// reportError reports a processor outcome that has an error: the status text
// as the reason, and "rejected" or "failed" from the class of the outcome.
func (o *searchOutcome) reportError(result seasonpack.Outcome) {
	o.Reason = result.Status.String()
	o.Status = "failed"
	if result.Class() == seasonpack.Rejected {
		o.Status = "rejected"
	}
}

// resultTorrent gets the torrent of one search result for the match. The
// clients of the result share it, so the run reads the stored torrent cache or
// downloads the torrent one time.
type resultTorrent struct {
	run     *discoveryRun
	indexer prowlarr.Indexer
	result  prowlarr.Result

	data        []byte
	downloadErr error
	// storageErr is a database failure, which stops the run.
	storageErr error
}

// fetch is the seasonpack.FetchFunc of the result. The match calls it only
// when the client has an episode file on disk, so a result that cannot be
// used costs no tracker request.
func (rt *resultTorrent) fetch(ctx context.Context) ([]byte, error) {
	runner := rt.run.runner
	if rt.data == nil && rt.downloadErr == nil {
		key := state.MetadataKey(rt.indexer.ID, rt.result)
		rt.data, rt.storageErr = runner.state.Metadata(ctx, key, time.Now())
		if rt.storageErr != nil {
			return nil, rt.storageErr
		}
		if rt.data != nil {
			rt.run.report.TorrentCacheHits++
		} else {
			rt.data, rt.downloadErr = runner.provider.Download(ctx, rt.indexer.ID, rt.result)
			if rt.downloadErr == nil {
				rt.run.report.TorrentDownloads++
				if _, err := torrents.ParseInfo(rt.data); err == nil {
					if rt.storageErr = runner.state.PutMetadata(ctx, key, rt.data, time.Now()); rt.storageErr != nil {
						return nil, rt.storageErr
					}
				}
			}
		}
	}
	if rt.downloadErr != nil {
		return nil, rt.downloadErr
	}
	return rt.data, nil
}

func (r *searchRunner) recordCooldown(ctx context.Context, indexerID int, err error) error {
	if failure, ok := errors.AsType[*prowlarr.CooldownError](err); ok && failure.Until.After(r.cooldowns[indexerID]) {
		r.cooldowns[indexerID] = failure.Until
		return r.state.SetCooldown(ctx, indexerID, failure.Until)
	}
	return nil
}

func (r *searchRunner) stateError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	r.log.Error().Err(errtrace.WithStack(err)).Msg("database operation failed")
	return errDatabase
}

func (run *discoveryRun) storageFailure(err error) {
	run.storageFailed = true
	run.report.Failures = append(run.report.Failures, searchFailure{Reason: run.runner.stateError(err).Error()})
}
