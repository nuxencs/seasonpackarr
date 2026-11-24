// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/discovery"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/inventory"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/release"
	"github.com/nuxencs/seasonpackarr/internal/seasonpack"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"

	"github.com/autobrr/rls"
	"github.com/gin-gonic/gin"
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
	cfg       config.Provider
	inv       *inventory.Store
	log       logger.Logger
	processor *seasonpack.Processor
	noti      *notifier
	source    *discovery.Source
}

// discoveryRun holds the state of one manual search or RSS poll that is not
// the state of the source: the clients, the selected variants, and the report.
type discoveryRun struct {
	runner  *searchRunner
	source  *discovery.Run
	request SearchRequest
	report  *searchReport
	// clients holds one handle for each client that the run could read. Each
	// handle has the config snapshot of the run, so a config reload during the
	// run changes no matching rule, no import rule, and no adapter.
	clients  map[string]*inventory.Client
	selected map[string][]rls.Release
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
		if errors.Is(err, discovery.ErrRunActive) {
			status = http.StatusConflict
		} else if errors.Is(err, discovery.ErrDatabase) {
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
	snapshot := r.cfg.Snapshot()
	kind := discovery.KindSearch
	if rss {
		kind = discovery.KindRSS
	}
	source, err := r.source.Begin(ctx, snapshot.Search, kind)
	if err != nil {
		return report, err
	}
	defer source.End()
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
		for entries := range clientInventory.SeasonGroups() {
			// Each group is one season.
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
				query := discovery.Query{Title: parsed.Title, Season: key.Season}
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
	run := discoveryRun{
		runner:   r,
		source:   source,
		request:  req,
		report:   &report,
		clients:  handles,
		selected: make(map[string][]rls.Release),
	}
	// With no groups, the run sends no request to Prowlarr.
	if len(ordered) > 0 {
		indexers := source.Indexers(ctx)
		if rss {
			// Do not consume listings for clients absent from this inventory scan.
			if len(handles) != len(clients) {
				source.ClientUnreadable()
			}
			run.pollRSS(ctx, indexers, groups)
		} else {
			for _, group := range ordered {
				for _, indexer := range indexers {
					for listing := range source.Search(ctx, indexer, group.query) {
						run.evaluateListing(ctx, listing, group)
					}
				}
			}
		}
	}
	stats := source.Stats()
	report.Requests, report.TorrentDownloads, report.TorrentCacheHits = stats.FeedPages, stats.Downloads, stats.CacheHits
	// The client failures come first, as they happen before the first call to Prowlarr.
	for _, failure := range source.Failures() {
		report.Failures = append(report.Failures, searchFailure{Query: failure.Query, IndexerID: failure.IndexerID, Reason: failure.Reason})
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
		query   discovery.Query
		clients map[string]bool
	}
)

// evaluateListing checks the listing for each client of its group, then
// imports it or reports what the import would do.
func (run *discoveryRun) evaluateListing(ctx context.Context, listing *discovery.Listing, group *seasonSearch) {
	processor := run.runner.processor
	title := listing.Title()
	for _, name := range slices.Sorted(maps.Keys(group.clients)) {
		if ctx.Err() != nil || run.source.Err() != nil {
			return
		}
		client := run.clients[name]
		snapshot := client.Snapshot()
		parsed := rls.ParseString(title)
		outcome := searchOutcome{ClientName: name, Title: title, IndexerID: listing.IndexerID(), Status: "rejected"}
		duplicate := false
		for selectedClient, acceptedReleases := range run.selected {
			if torrentclient.EndpointOf(*snapshot.Clients[name]) != torrentclient.EndpointOf(*snapshot.Clients[selectedClient]) {
				continue
			}
			for _, accepted := range acceptedReleases {
				if release.SeasonKeyOf(&parsed, snapshot.FuzzyMatching) == release.SeasonKeyOf(&accepted, snapshot.FuzzyMatching) && release.CheckCandidates(parsed, accepted, snapshot.FuzzyMatching).StatusCode == domain.StatusAlreadyInClient {
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
			if checked := processor.Candidate(ctx, client, title); checked.Err != nil {
				outcome.reportError(checked)
			} else {
				outcome.Status = "candidate"
				outcome.Reason = "release checks passed; exact coverage not verified"
			}
			run.report.Outcomes = append(run.report.Outcomes, outcome)
			continue
		}
		// The match fetches only with a client episode file on disk. The closure
		// keeps the result for the reason of a failed download and for the import.
		var (
			torrent  []byte
			fetchErr error
		)
		matched := processor.Match(ctx, client, title, seasonpack.TorrentFetch(func(ctx context.Context) ([]byte, error) {
			torrent, fetchErr = listing.Torrent(ctx)
			return torrent, fetchErr
		}))
		// A database failure stops the run, and the source has its failure entry.
		// A failed download keeps its outcome, also when its cooldown was not stored.
		if run.source.Err() != nil && !errors.Is(fetchErr, discovery.ErrDownload) {
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
			case fetchErr != nil && errors.Is(matched.Err, fetchErr):
				outcome.Status = "failed"
				outcome.Reason = fetchErr.Error()
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
			imported := processor.Import(ctx, client, title, torrent)
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
			log := run.runner.log.With().Str("module", "processor").Str("release", title).Str("clientname", name).Logger()
			run.runner.noti.send(log, imported.Status, domain.NotificationPayload{
				ReleaseName: title,
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
