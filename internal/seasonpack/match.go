// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package seasonpack

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/inventory"
	"github.com/nuxencs/seasonpackarr/internal/placement"
	"github.com/nuxencs/seasonpackarr/internal/release"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrents"
)

// importPlan is the result of an accepted match. Only buildPlan makes one, so
// each plan passed the coverage rule when it was built.
type importPlan struct {
	torrentBytes []byte
	hashes       torrents.Hashes
	info         torrents.Info
	links        []placement.PlannedLink
	totalEps     int
}

func (plan importPlan) coverage() Coverage {
	return Coverage{Reusable: len(plan.links), Total: plan.totalEps}
}

// Match checks the torrent against the client episode files on disk. It has no
// filesystem or client side effects. An accepted match keeps its import plan
// for the import. A rejection has the coverage when the torrent episodes were
// matched, and keeps no plan.
func (p *Processor) Match(ctx context.Context, client *inventory.Client, releaseName string, torrent Torrent) Outcome {
	c := p.newCall(client, releaseName)
	c.log.Info().Msgf("using %s client", client.Name())

	plan, unmatched, outcome := c.buildPlan(ctx, torrent)
	if outcome.Coverage != nil {
		c.logCoverage(*outcome.Coverage, unmatched)
	}
	if outcome.Err != nil {
		return outcome
	}

	p.plans.store(c.planScope(), plan)

	return outcome
}

func (c *call) logCoverage(coverage Coverage, unmatchedEpisodes []release.EpisodeUnmatched) {
	for _, unmatched := range unmatchedEpisodes {
		event := c.log.Info().
			Str("torrent_path", unmatched.TorrentPath).
			Str("reason", string(unmatched.Reason))
		if unmatched.ClosestClientPath != "" {
			event = event.Str("closest_client_path", unmatched.ClosestClientPath)
		}
		if len(unmatched.Mismatches) > 0 {
			mismatches := make([]map[string]any, len(unmatched.Mismatches))
			for index, mismatch := range unmatched.Mismatches {
				mismatches[index] = map[string]any{
					"field": string(mismatch.Field),
					"want":  mismatch.Want,
					"got":   mismatch.Got,
				}
			}
			event = event.Interface("mismatches", mismatches)
		}
		event.Msg("torrent episode is not reusable")
	}

	summary := c.log.Info().
		Int("reusable_episodes", coverage.Reusable).
		Int("total_episodes", coverage.Total).
		Int("unmatched_episodes", len(unmatchedEpisodes)).
		Float32("coverage_percent", coverage.percent()*100)
	if c.cfg.SmartMode {
		summary = summary.Float32("threshold_percent", c.cfg.SmartModeThreshold*100)
	}
	summary.Msg("season pack import plan built")
}

// buildPlan runs the candidate check, matches the torrent episodes to the
// client episode files on disk, and applies the coverage rule. Only an accepted
// season pack gets a plan. The unmatched episodes are for the log of the match.
func (c *call) buildPlan(ctx context.Context, torrent Torrent) (importPlan, []release.EpisodeUnmatched, Outcome) {
	if torrent.fetch == nil && len(torrent.bytes) == 0 {
		return importPlan{}, nil, outcomeOf(domain.StatusTorrentBytesError)
	}

	candidates, outcome := c.findCandidates(ctx)
	if outcome.Err != nil {
		return importPlan{}, nil, outcome
	}

	var (
		parsed  parsedTorrent
		sources []release.EpisodeFile
	)
	if torrent.fetch == nil {
		if parsed, outcome = parseTorrent(torrent.bytes); outcome.Err != nil {
			return importPlan{}, nil, outcome
		}
		// readSources logged each failed read. See TorrentBytes for the reason to continue.
		sources, _ = c.readSources(ctx, candidates)
	} else if sources, parsed, outcome = c.fetchTorrent(ctx, candidates, torrent.fetch); outcome.Err != nil {
		return importPlan{}, nil, outcome
	}

	matchResult := release.NewEpisodeMatcher(parsed.episodes).Match(sources)
	links := make([]placement.PlannedLink, 0, len(matchResult.Matches))
	for _, match := range matchResult.Matches {
		links = append(links, placement.PlannedLink{
			Source:      match.ClientPath,
			TorrentPath: match.TorrentPath,
		})
	}

	plan := importPlan{
		torrentBytes: parsed.bytes,
		hashes:       parsed.hashes,
		info:         parsed.info,
		links:        links,
		totalEps:     len(parsed.episodes),
	}
	coverage := plan.coverage()
	switch {
	case len(links) == 0:
		return importPlan{}, matchResult.Unmatched, outcomeOf(domain.StatusFailedMatchToTorrentEps).withCoverage(coverage)
	case !coverage.meetsThreshold(c.cfg):
		return importPlan{}, matchResult.Unmatched, outcomeOf(domain.StatusBelowThreshold).withCoverage(coverage)
	}
	return plan, matchResult.Unmatched, Outcome{Status: domain.StatusSuccessfulMatch}.withCoverage(coverage)
}

// fetchTorrent reads the client episode files, then gets the torrent with
// fetch. See TorrentFetch for the two stops before the fetch.
func (c *call) fetchTorrent(ctx context.Context, candidates []inventory.Entry, fetch FetchFunc) ([]release.EpisodeFile, parsedTorrent, Outcome) {
	sources, err := c.readSources(ctx, candidates)
	if err != nil {
		return nil, parsedTorrent{}, Outcome{
			Status: domain.StatusGetTorrentsError,
			Err:    fmt.Errorf("%w: %w", ErrFileDetailRead, err),
		}
	}
	if len(sources) == 0 {
		return nil, parsedTorrent{}, Outcome{Status: domain.StatusFailedMatchToTorrentEps, Err: ErrNoSourceOnDisk}
	}

	data, err := fetch(ctx)
	if err != nil {
		return nil, parsedTorrent{}, outcomeOfError(domain.StatusTorrentBytesError, err)
	}
	if len(data) == 0 {
		return nil, parsedTorrent{}, outcomeOf(domain.StatusTorrentBytesError)
	}

	parsed, outcome := parseTorrent(data)
	return sources, parsed, outcome
}

// parsedTorrent is a torrent with the episodes that count for the coverage.
type parsedTorrent struct {
	bytes    []byte
	hashes   torrents.Hashes
	info     torrents.Info
	episodes []release.EpisodeFile
}

func parseTorrent(data []byte) (parsedTorrent, Outcome) {
	torrentInfo, err := torrents.ParseInfo(data)
	if err != nil {
		return parsedTorrent{}, outcomeOfError(domain.StatusParseTorrentInfoError, err)
	}

	torrentEps, err := torrentInfo.Episodes()
	if err != nil {
		return parsedTorrent{}, outcomeOfError(domain.StatusGetEpisodesError, err)
	}
	eligibleEps := make([]release.EpisodeFile, 0, len(torrentEps))
	for _, torrentEp := range torrentEps {
		if episode, ok := release.ParseEpisodeFile(torrentEp.Path, torrentEp.Size); ok {
			eligibleEps = append(eligibleEps, episode)
		}
	}
	if len(eligibleEps) == 0 {
		return parsedTorrent{}, outcomeOf(domain.StatusFailedMatchToTorrentEps)
	}

	hashes, err := torrents.InfoHashes(data)
	if err != nil {
		return parsedTorrent{}, outcomeOfError(domain.StatusParseTorrentInfoError, err)
	}

	return parsedTorrent{bytes: data, hashes: hashes, info: torrentInfo, episodes: eligibleEps}, Outcome{Status: domain.StatusSuccessfulMatch}
}

// readSources reads the episode file of each candidate from the client and
// keeps the files that are on disk. The error holds each failed file-detail
// read. A file that is not on disk is no error.
func (c *call) readSources(ctx context.Context, candidates []inventory.Entry) ([]release.EpisodeFile, error) {
	var failures []error
	hashes := make([]string, len(candidates))
	for index, candidate := range candidates {
		hashes[index] = candidate.Torrent.Hash
	}

	results := c.client.Adapter().GetFiles(ctx, hashes)
	episodes := make([]release.EpisodeFile, 0, len(results))
	for index, candidate := range candidates {
		if index >= len(results) {
			failures = append(failures, errors.New("torrent client omitted a file result"))
			c.log.Error().Msgf("torrent client omitted file result for %s", candidate.Torrent.Name)
			continue
		}

		result := results[index]
		if !strings.EqualFold(result.Hash, candidate.Torrent.Hash) {
			failures = append(failures, errors.New("torrent client returned an out-of-order file result"))
			c.log.Error().Msgf("torrent client returned out-of-order file result for %s", candidate.Torrent.Name)
			continue
		}
		if result.Err != nil {
			failures = append(failures, result.Err)
			c.log.Error().Err(result.Err).Msgf("error getting file info: %s", candidate.Torrent.Name)
			continue
		}

		episode, err := episodeFileFromFiles(result.Files, candidate.Torrent.SavePath)
		if err != nil {
			c.log.Error().Err(err).Msgf("error getting file info: %s", candidate.Torrent.Name)
			continue
		}
		if !c.onDisk(episode) {
			continue
		}
		episodes = append(episodes, episode)
	}
	return episodes, errors.Join(failures...)
}

// onDisk reports whether source is a regular file with the declared size, and
// logs the reason when it is not. The inventory has no download state, so a
// running episode download is a normal cause. The check does not prove that the
// download is complete: a file that the client preallocated to its full size
// passes. The torrent client still verifies the linked data during the import.
func (c *call) onDisk(source release.EpisodeFile) bool {
	info, err := os.Stat(source.Path())
	if err == nil && info.Mode().IsRegular() && info.Size() == source.Size() {
		return true
	}

	event := c.log.Info().Str("source", source.Path())
	switch {
	case err != nil:
		event = event.Str("reason", "missing")
		// Another stat error, such as a denied permission, needs its own text.
		if !errors.Is(err, fs.ErrNotExist) {
			event = event.Err(err)
		}
	case !info.Mode().IsRegular():
		event = event.Str("reason", "not_regular_file")
	default:
		event = event.Str("reason", "size_mismatch").
			Int64("want_size", source.Size()).
			Int64("got_size", info.Size())
	}
	event.Msg("client episode file is not reusable")
	return false
}

func episodeFileFromFiles(files []torrentclient.File, savePath string) (release.EpisodeFile, error) {
	for _, f := range files {
		episode, ok := release.ParseEpisodeFile(filepath.Join(savePath, f.Name), f.Size)
		if !ok {
			continue
		}
		if f.Size == 0 {
			return release.EpisodeFile{}, errors.New("file size is empty")
		}
		return episode, nil
	}

	return release.EpisodeFile{}, errors.New("file name is empty")
}
