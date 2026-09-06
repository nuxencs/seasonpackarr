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
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/autobrr/rls"
	"github.com/gin-gonic/gin"
	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/format"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/prowlarr"
	"github.com/nuxencs/seasonpackarr/internal/release"
	"github.com/nuxencs/seasonpackarr/internal/torrents"
)

var errSearchRunning = errors.New("a backfill run is already active")

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
	log         logger.Logger
	noti        domain.Sender
	tasks       *taskGroup
	running     atomic.Bool
	cooldowns   map[int]time.Time
	metadata    searchMetadataCache
	prowlarrURL string
	prowlarrKey string
}

// A run uses one immutable config snapshot, including matching and import rules.
type searchConfig struct{ value domain.Config }

func (s searchConfig) Snapshot() domain.Config { return s.value }

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
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, report)
}

func (r *searchRunner) run(ctx context.Context, req SearchRequest) (searchReport, error) {
	report := searchReport{DryRun: req.DryRun, Verify: req.Verify, Outcomes: []searchOutcome{}, Failures: []searchFailure{}}
	if req.Verify && !req.DryRun {
		return report, errors.New("verify requires dryRun")
	}
	if !r.running.CompareAndSwap(false, true) {
		return report, errSearchRunning
	}
	defer r.running.Store(false)
	snapshot := r.cfg.Snapshot()
	spacing, err := time.ParseDuration(snapshot.Search.RequestInterval)
	if err != nil {
		return report, errors.New("invalid search.requestInterval")
	}
	provider, err := prowlarr.New(snapshot.Search.ProwlarrURL, snapshot.Search.APIKey, spacing)
	if err != nil {
		return report, err
	}
	if r.prowlarrURL != snapshot.Search.ProwlarrURL || r.prowlarrKey != snapshot.Search.APIKey {
		r.cooldowns = make(map[int]time.Time)
		r.metadata = searchMetadataCache{}
		r.prowlarrURL = snapshot.Search.ProwlarrURL
		r.prowlarrKey = snapshot.Search.APIKey
	}
	// Keep only active deadlines from earlier runs. New failures remain in this
	// map for the current run, even when Retry-After is zero or already elapsed.
	maps.DeleteFunc(r.cooldowns, func(_ int, until time.Time) bool { return !until.After(time.Now()) })
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
			r.log.Warn().Str("clientname", failure.ClientName).Str("query", failure.Query).Int("indexer_id", failure.IndexerID).Str("reason", failure.Reason).Msg("backfill operation failed")
		}
		for _, outcome := range report.Outcomes {
			r.log.Info().Str("clientname", outcome.ClientName).Str("release", outcome.Title).Int("indexer_id", outcome.IndexerID).Str("status", outcome.Status).Str("reason", outcome.Reason).Interface("reusable_episodes", outcome.ReusableEpisodes).Interface("total_episodes", outcome.TotalEpisodes).Msg("backfill result")
		}
		r.log.Info().Bool("dry_run", req.DryRun).Bool("verify", req.Verify).Int("torrent_downloads", report.TorrentDownloads).Int("torrent_cache_hits", report.TorrentCacheHits).Int("covered_episode_torrents", report.CoveredEpisodeTorrents).Int("groups", report.Groups).Int("search_requests", report.Requests).Int("outcomes", len(report.Outcomes)).Int("failures", len(report.Failures)).Int64("duration_ms", time.Since(started).Milliseconds()).Msg("backfill run completed")
	}()

	processors := make(map[string]*processor, len(clients))
	groups := make(map[seasonSearchKey]*seasonSearch)
	for _, name := range clients {
		if ctx.Err() != nil {
			break
		}
		p := newProcessor(r.log, searchConfig{snapshot}, r.noti, r.tasks)
		p.req = &request{ClientName: name}
		cfg := snapshot.Clients[name]
		if err := p.getClient(ctx, cfg, name); err != nil {
			report.Failures = append(report.Failures, searchFailure{ClientName: name, Reason: "could not connect to torrent client"})
			continue
		}
		// Search starts from fresh summaries. Exact planning can reuse this scan.
		entryMap.Delete(name)
		inventory, err := p.getAllTorrents(ctx, name, cfg, snapshot.FuzzyMatching)
		if err != nil {
			report.Failures = append(report.Failures, searchFailure{ClientName: name, Reason: "could not read torrent client inventory"})
			continue
		}
		processors[name] = p
		for _, entries := range inventory {
			// Inventory buckets already apply the title, season, and year rules.
			// Collect packs once so episode-only seasons do not need pairwise scans.
			var packs []*rls.Release
			for _, entry := range entries {
				if entry.release.Type.Is(rls.Series) && entry.release.Episode == 0 {
					packs = append(packs, entry.release)
				}
			}
			for _, entry := range entries {
				report.ScannedTorrents++
				parsed := entry.release
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
				key := seasonSearchKey{Title: rls.MustNormalize(parsed.Title), Year: parsed.Year, Season: parsed.Series}
				if snapshot.FuzzyMatching.SkipYearCompare {
					key.Year = 0
				}
				group := groups[key]
				query := prowlarr.Query{Title: parsed.Title, Year: key.Year, Season: key.Season}
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
	if until := r.cooldowns[0]; time.Now().Before(until) {
		report.Failures = append(report.Failures, searchFailure{Reason: "Prowlarr cooldown is still active"})
		return report, nil
	}
	indexers, err := provider.Indexers(ctx)
	if err != nil {
		r.recordCooldown(0, err)
		report.Failures = append(report.Failures, searchFailure{Reason: err.Error()})
		return report, nil
	}
	if len(snapshot.Search.IndexerIDs) > 0 {
		for _, id := range snapshot.Search.IndexerIDs {
			if !slices.ContainsFunc(indexers, func(indexer prowlarr.Indexer) bool { return indexer.ID == id }) {
				report.Failures = append(report.Failures, searchFailure{IndexerID: id, Reason: "selected indexer is missing, disabled, or not a searchable torrent indexer"})
			}
		}
		indexers = slices.DeleteFunc(indexers, func(indexer prowlarr.Indexer) bool { return !slices.Contains(snapshot.Search.IndexerIDs, indexer.ID) })
	}
	if len(indexers) == 0 {
		report.Failures = append(report.Failures, searchFailure{Reason: "no enabled searchable torrent indexers in Prowlarr"})
		return report, nil
	}
	selected := make(map[string][]rls.Release)
	unavailable := make(map[int]bool)
	for _, indexer := range indexers {
		if until := r.cooldowns[indexer.ID]; time.Now().Before(until) {
			unavailable[indexer.ID] = true
			report.Failures = append(report.Failures, searchFailure{IndexerID: indexer.ID, Reason: "indexer cooldown or Retry-After deadline has not elapsed"})
		}
	}
	for _, group := range ordered {
		for _, indexer := range indexers {
			if ctx.Err() != nil {
				break
			}
			if unavailable[indexer.ID] {
				continue
			}
			r.searchGroup(ctx, provider, indexer, group, processors, selected, unavailable, req, &report)
		}
		if ctx.Err() != nil {
			break
		}
	}
	if ctx.Err() != nil {
		report.Failures = append(report.Failures, searchFailure{Reason: ctx.Err().Error()})
	}
	return report, nil
}

type (
	seasonSearchKey struct {
		Title        string
		Year, Season int
	}
	seasonSearch struct {
		query   prowlarr.Query
		clients map[string]bool
	}
)

func (r *searchRunner) searchGroup(ctx context.Context, provider *prowlarr.Client, indexer prowlarr.Indexer, group *seasonSearch, processors map[string]*processor, selected map[string][]rls.Release, unavailable map[int]bool, req SearchRequest, report *searchReport) {
	seen := make(map[string]bool)
	offset := 0
	const maxPages = 10
	for range maxPages {
		report.Requests++
		results, limit, err := provider.SearchPage(ctx, indexer, group.query, offset)
		if err != nil {
			report.Failures = append(report.Failures, searchFailure{Query: group.query.String(), IndexerID: indexer.ID, Reason: err.Error()})
			// Do not hammer an unavailable or rate-limited indexer for every season.
			unavailable[indexer.ID] = true
			r.recordCooldown(indexer.ID, err)
			return
		}
		fresh := 0
		for _, result := range results {
			if ctx.Err() != nil {
				return
			}
			id := cmp.Or(result.GUID, result.Link, result.Enclosure.URL, result.Title)
			if seen[id] {
				continue
			}
			seen[id] = true
			fresh++
			r.evaluateResult(ctx, provider, indexer, result, group, processors, selected, req, report)
			if _, failed := r.cooldowns[indexer.ID]; failed {
				unavailable[indexer.ID] = true
				return
			}
		}
		if !indexer.SupportsPagination || len(results) < limit {
			return
		}
		if fresh == 0 {
			report.Failures = append(report.Failures, searchFailure{Query: group.query.String(), IndexerID: indexer.ID, Reason: "indexer repeated a page; remaining results may be incomplete"})
			return
		}
		offset += len(results)
	}
	report.Failures = append(report.Failures, searchFailure{Query: group.query.String(), IndexerID: indexer.ID, Reason: "search reached the 10-page limit; remaining results may be incomplete"})
}

func (r *searchRunner) evaluateResult(ctx context.Context, provider *prowlarr.Client, indexer prowlarr.Indexer, result prowlarr.Result, group *seasonSearch, processors map[string]*processor, selected map[string][]rls.Release, req SearchRequest, report *searchReport) {
	var data []byte
	var downloadErr error
	for _, name := range slices.Sorted(maps.Keys(group.clients)) {
		if ctx.Err() != nil {
			return
		}
		p := processors[name]
		snapshot := p.cfg.Snapshot()
		parsed := rls.ParseString(result.Title)
		outcome := searchOutcome{ClientName: name, Title: result.Title, IndexerID: indexer.ID, Status: "rejected"}
		p.req.Name = result.Title
		p.req.Torrent = nil
		duplicate := false
		for selectedClient, acceptedReleases := range selected {
			if !sameImportEndpoint(*snapshot.Clients[name], *snapshot.Clients[selectedClient]) {
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
			report.Outcomes = append(report.Outcomes, outcome)
			continue
		}
		candidates, status, err := p.findCandidates(ctx, name, snapshot.Clients[name], snapshot)
		if err != nil {
			outcome.Reason = status.String()
			if !isExpectedGateRejection(status) {
				outcome.Status = "failed"
			}
			report.Outcomes = append(report.Outcomes, outcome)
			continue
		}
		if req.DryRun && !req.Verify {
			outcome.Status = "candidate"
			outcome.Reason = "release checks passed; exact coverage not verified"
			report.Outcomes = append(report.Outcomes, outcome)
			continue
		}
		sources, err := p.availableSearchSources(ctx, candidates)
		if err != nil {
			outcome.Status = "failed"
			outcome.Reason = "could not read episode file details from torrent client"
			report.Outcomes = append(report.Outcomes, outcome)
			continue
		}
		if len(sources) == 0 {
			outcome.Reason = "no accessible episode files with the declared size"
			report.Outcomes = append(report.Outcomes, outcome)
			continue
		}
		if data == nil && downloadErr == nil {
			key := metadataKey(indexer.ID, result)
			data = r.metadata.get(key, time.Now())
			if data != nil {
				report.TorrentCacheHits++
			} else {
				data, downloadErr = provider.Download(ctx, indexer.ID, result)
				if downloadErr == nil {
					report.TorrentDownloads++
					if _, err := torrents.Info(data); err == nil {
						r.metadata.put(key, data, time.Now())
					}
				}
			}
		}
		if downloadErr != nil {
			r.recordCooldown(indexer.ID, downloadErr)
			outcome.Status = "failed"
			outcome.Reason = downloadErr.Error()
			report.Outcomes = append(report.Outcomes, outcome)
			continue
		}
		p.req.Torrent, _ = json.Marshal(data)
		plan, status, err := p.buildImportPlanWithSources(ctx, name, snapshot.Clients[name], snapshot, sources)
		if plan.totalEps > 0 {
			outcome.ReusableEpisodes = new(len(plan.links))
			outcome.TotalEpisodes = new(plan.totalEps)
		}
		if err == nil && !plan.meetsPlannedCoverageThreshold(snapshot) {
			status = domain.StatusBelowThreshold
			err = status.Error()
		}
		if err != nil {
			outcome.Reason = status.String()
			if !isExpectedGateRejection(status) {
				outcome.Status = "failed"
			}
			report.Outcomes = append(report.Outcomes, outcome)
			continue
		}
		if req.DryRun {
			outcome.Status = "would_import"
			selected[name] = append(selected[name], parsed)
		} else {
			p.storeImportPlan(name, *snapshot.Clients[name], snapshot.FuzzyMatching, plan)
			status, err = p.importSeasonPack(ctx)
			if err != nil {
				outcome.Status = "failed"
				outcome.Reason = status.String()
				if isExpectedGateRejection(status) {
					outcome.Status = "rejected"
				}
			} else {
				outcome.Status = "imported"
			}
			// After an import attempt, a client may already contain the pack even if
			// verification/resume failed. Leave recovery to the next inventory scan.
			selected[name] = append(selected[name], parsed)
			p.sendNotification(status, "Backfill", err)
		}
		report.Outcomes = append(report.Outcomes, outcome)
	}
}

func (r *searchRunner) recordCooldown(indexerID int, err error) {
	if failure, ok := errors.AsType[*prowlarr.CooldownError](err); ok && failure.Until.After(r.cooldowns[indexerID]) {
		r.cooldowns[indexerID] = failure.Until
	}
}

// This preflight does not prove completion or piece validity. The torrent client
// still verifies linked data during import.
func (p *processor) availableSearchSources(ctx context.Context, candidates []entry) ([]release.EpisodeFile, error) {
	sources, err := p.getEpisodeFilesChecked(ctx, candidates)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(sources, func(source release.EpisodeFile) bool {
		info, err := os.Stat(source.Path())
		return err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() != source.Size()
	}), nil
}
