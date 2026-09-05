// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	stdslices "slices"
	"sync"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/files"
	"github.com/nuxencs/seasonpackarr/internal/format"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/metadata"
	"github.com/nuxencs/seasonpackarr/internal/release"
	"github.com/nuxencs/seasonpackarr/internal/slices"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrents"
	"github.com/nuxencs/seasonpackarr/pkg/errors"

	"github.com/autobrr/rls"
	"github.com/gin-gonic/gin"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/rs/zerolog"
)

type processor struct {
	log  zerolog.Logger
	cfg  config.Provider
	noti domain.Sender
	meta *metadata.Provider
	req  *request
}

type request struct {
	Name       string
	Torrent    json.RawMessage
	Client     torrentclient.TorrentClient
	ClientName string
}

type entry struct {
	torrent torrentclient.Torrent
	release rls.Release
}

type entryCache struct {
	entriesMap    map[string][]entry
	rlsMap        map[string]rls.Release
	clientConfig  domain.Client
	fuzzyMatching domain.FuzzyMatching
	lastUpdated   time.Time
	mu            sync.Mutex
}

type matchInfo struct {
	clientEpPath string
	clientEpSize int64
}

type cachedTorrentClient struct {
	config domain.Client
	client torrentclient.TorrentClient
}

var (
	clientMap = xsync.NewMapOf[string, cachedTorrentClient]()
	entryMap  = xsync.NewMapOf[string, *entryCache]()
)

func newProcessor(log logger.Logger, config config.Provider, notification domain.Sender, metadata *metadata.Provider) *processor {
	return &processor{
		log:  log.With().Str("module", "processor").Logger(),
		cfg:  config,
		noti: notification,
		meta: metadata,
	}
}

func (p *processor) getClient(client *domain.Client, clientName string) error {
	// allow tests (and repeated calls within a request) to inject/keep a client
	if p.req.Client != nil {
		return nil
	}

	if cached, ok := clientMap.Load(clientName); ok && clientConfigsEqual(cached.config, *client) {
		p.req.Client = cached.client
		return nil
	}

	c, err := torrentclient.New(client)
	if err != nil {
		return errors.Wrap(err, "failed to create torrent client")
	}

	clientMap.Store(clientName, cachedTorrentClient{
		config: cloneClientConfig(*client),
		client: c,
	})
	p.req.Client = c
	return nil
}

func cloneClientConfig(client domain.Client) domain.Client {
	clone := client
	clone.Import.Tags = stdslices.Clone(client.Import.Tags)
	return clone
}

func clientConfigsEqual(a, b domain.Client) bool {
	return a.Type == b.Type &&
		a.Host == b.Host &&
		a.Port == b.Port &&
		a.Username == b.Username &&
		a.Password == b.Password &&
		a.APIKey == b.APIKey &&
		a.Import.SavePath == b.Import.SavePath &&
		stdslices.Equal(a.Import.Tags, b.Import.Tags) &&
		a.Import.Category == b.Import.Category &&
		a.Import.DownloadPath == b.Import.DownloadPath &&
		a.Import.ContentLayout == b.Import.ContentLayout
}

func (p *processor) getAllTorrents(clientName string, clientConfig *domain.Client, fuzzyMatching domain.FuzzyMatching) (map[string][]entry, error) {
	f := func() *entryCache {
		tre, ok := entryMap.Load(clientName)
		if ok && clientConfigsEqual(tre.clientConfig, *clientConfig) && tre.fuzzyMatching == fuzzyMatching {
			return tre
		}

		entries := &entryCache{
			rlsMap:        make(map[string]rls.Release),
			clientConfig:  cloneClientConfig(*clientConfig),
			fuzzyMatching: fuzzyMatching,
		}
		entryMap.Store(clientName, entries)
		return entries
	}

	entries := f()
	cur := time.Now()
	if entries.lastUpdated.After(cur) {
		return entries.entriesMap, nil
	}

	entries.mu.Lock()
	defer entries.mu.Unlock()

	if entries.lastUpdated.After(cur) {
		return entries.entriesMap, nil
	}

	ts, err := p.req.Client.GetTorrents()
	if err != nil {
		return nil, err
	}

	after := time.Now()
	refreshed := &entryCache{
		entriesMap:    make(map[string][]entry),
		rlsMap:        entries.rlsMap,
		clientConfig:  cloneClientConfig(*clientConfig),
		fuzzyMatching: fuzzyMatching,
		lastUpdated:   after.Add(after.Sub(cur)),
	}

	for _, t := range ts {
		r, ok := refreshed.rlsMap[t.Name]
		if !ok {
			r = rls.ParseString(t.Name)
			refreshed.rlsMap[t.Name] = r
		}

		comparableTitle := format.ComparableTitle(r, fuzzyMatching)
		refreshed.entriesMap[comparableTitle] = append(refreshed.entriesMap[comparableTitle], entry{torrent: t, release: r})
	}

	entryMap.Compute(clientName, func(current *entryCache, loaded bool) (*entryCache, bool) {
		if loaded && current != entries {
			return current, false
		}
		return refreshed, false
	})
	return refreshed.entriesMap, nil
}

func (p *processor) getFiles(hash string) (string, int64, error) {
	torrentFiles, err := p.req.Client.GetFiles(hash)
	if err != nil {
		return "", 0, err
	}

	var fileName string
	var size int64
	for _, f := range torrentFiles {
		if !release.IsValidEpisodeFile(f.Name) {
			continue
		}

		fileName = f.Name
		size = f.Size
		break
	}
	switch {
	case len(fileName) == 0:
		return "", 0, errors.New("file name is empty")
	case size == 0:
		return "", 0, errors.New("file size is empty")
	}

	return fileName, size, nil
}

func (p *processor) getClientName() string {
	if len(p.req.ClientName) == 0 {
		p.req.ClientName = "default"
		p.log.Info().Msg("no clientname defined. trying to use default client")

		return "default"
	}

	return p.req.ClientName
}

func (p *processor) ProcessSeasonPackHandler(c *gin.Context) {
	p.log.Info().Msg("starting to process season pack request")

	if err := json.NewDecoder(c.Request.Body).Decode(&p.req); err != nil {
		p.log.Error().Err(err).Msgf("%s", domain.StatusDecodingError)
		c.AbortWithStatusJSON(domain.StatusDecodingError.Code(), gin.H{
			"statusCode": domain.StatusDecodingError.Code(),
			"error":      err.Error(),
		})
		return
	}

	statusCode, err := p.processSeasonPack()
	if err != nil {
		go func() {
			if sendErr := p.noti.Send(statusCode, domain.NotificationPayload{
				ReleaseName: p.req.Name,
				Client:      p.req.ClientName,
				Action:      "Pack",
				Error:       err,
			}); sendErr != nil {
				p.log.Error().Err(sendErr).Msgf("error sending %s notification for %d", p.noti.Name(), statusCode)
			}
		}()

		p.log.Error().Err(err).Msg("error processing season pack")
		c.AbortWithStatusJSON(statusCode.Code(), gin.H{
			"statusCode": statusCode.Code(),
			"error":      err.Error(),
		})
		return
	}

	go func() {
		if sendErr := p.noti.Send(statusCode, domain.NotificationPayload{
			ReleaseName: p.req.Name,
			Client:      p.req.ClientName,
			Action:      "Pack",
		}); sendErr != nil {
			p.log.Error().Err(sendErr).Msgf("error sending %s notification for %d", p.noti.Name(), statusCode)
		}
	}()

	p.log.Info().Msg("successfully matched season pack to episodes in client")
	c.String(statusCode.Code(), statusCode.String())
}

// processSeasonPack is the /api/pack gate: it only decides whether the announced
// season pack matches existing client episodes. It has no filesystem side
// effects - hardlinking and importing happen on /api/parse.
func (p *processor) processSeasonPack() (domain.StatusCode, error) {
	clientName := p.getClientName()
	snapshot := p.cfg.Snapshot()

	p.log.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Str("release", p.req.Name).Str("clientname", clientName)
	})

	clientCfg, ok := snapshot.Clients[clientName]
	if !ok {
		return domain.StatusClientNotFound, domain.StatusClientNotFound.Error()
	}
	p.log.Info().Msgf("using %s client", clientName)

	if _, statusCode, err := p.collectMatches(clientName, clientCfg, snapshot); err != nil {
		return statusCode, err
	}

	return domain.StatusSuccessfulMatch, nil
}

// collectMatches resolves the announced release against the episodes currently
// in the client and returns the deduped set of matched client episodes. It is
// shared by /api/pack (gate) and /api/parse (import) so the two never disagree.
func (p *processor) collectMatches(clientName string, clientCfg *domain.Client, cfg domain.Config) ([]matchInfo, domain.StatusCode, error) {
	if len(p.req.Name) == 0 {
		return nil, domain.StatusAnnounceNameError, domain.StatusAnnounceNameError.Error()
	}

	if err := p.getClient(clientCfg, clientName); err != nil {
		return nil, domain.StatusGetClientError, fmt.Errorf("%s: %w", domain.StatusGetClientError, err)
	}

	entries, err := p.getAllTorrents(clientName, clientCfg, cfg.FuzzyMatching)
	if err != nil {
		return nil, domain.StatusGetTorrentsError, fmt.Errorf("%s: %w", domain.StatusGetTorrentsError, err)
	}

	requestRls := rls.ParseString(p.req.Name)
	filteredEntries, ok := entries[format.ComparableTitle(requestRls, cfg.FuzzyMatching)]
	if !ok {
		return nil, domain.StatusNoMatches, domain.StatusNoMatches.Error()
	}

	comparisons := make([]domain.CompareInfo, len(filteredEntries))
	for i, filteredEntry := range filteredEntries {
		compareInfo := release.CheckCandidates(requestRls, filteredEntry.release, cfg.FuzzyMatching)
		comparisons[i] = compareInfo
		switch compareInfo.StatusCode {
		case domain.StatusAlreadyInClient, domain.StatusNotASeasonPack:
			return nil, compareInfo.StatusCode, compareInfo.StatusCode.Error()
		}
	}

	codeSet := make(map[domain.StatusCode]bool)
	epsSet := make(map[int]struct{})
	matches := make([]matchInfo, 0, len(filteredEntries))

	for i, filteredEntry := range filteredEntries {
		compareInfo := comparisons[i]
		switch compareInfo.StatusCode {
		case domain.StatusResolutionMismatch, domain.StatusSourceMismatch, domain.StatusRlsGrpMismatch,
			domain.StatusCutMismatch, domain.StatusEditionMismatch, domain.StatusRepackStatusMismatch,
			domain.StatusHdrMismatch, domain.StatusStreamingServiceMismatch:
			p.log.Info().Msgf("%s: request(%s => %v), client(%s => %v)",
				compareInfo.StatusCode, requestRls.String(), compareInfo.RejectValueA,
				filteredEntry.release.String(), compareInfo.RejectValueB)
			codeSet[compareInfo.StatusCode] = true
			continue

		case domain.StatusSuccessfulMatch:
			fileName, size, err := p.getFiles(filteredEntry.torrent.Hash)
			if err != nil {
				p.log.Error().Err(err).Msgf("error getting file info: %s", filteredEntry.torrent.Name)
				continue
			}

			clientEpPath := filepath.Join(filteredEntry.torrent.SavePath, fileName)

			epsSet[filteredEntry.release.Episode] = struct{}{}
			matches = append(matches, matchInfo{
				clientEpPath: clientEpPath,
				clientEpSize: size,
			})

			p.log.Debug().Msgf("matched torrent from client: name(%s), size(%d), hash(%s)",
				filteredEntry.torrent.Name, size, filteredEntry.torrent.Hash)
			codeSet[compareInfo.StatusCode] = true
		}
	}

	if !codeSet[domain.StatusSuccessfulMatch] {
		return nil, domain.StatusNoMatches, domain.StatusNoMatches.Error()
	}

	if cfg.SmartMode {
		totalEps, err := p.meta.EpisodesInSeason(requestRls)
		if err != nil {
			return nil, domain.StatusEpisodeCountError, fmt.Errorf("%s: %w", domain.StatusEpisodeCountError, err)
		}

		foundEps := len(epsSet)
		percentEps := release.PercentOfTotalEpisodes(totalEps, foundEps)

		p.log.Info().Msgf("found %d/%d (%.2f%%) episodes in client", foundEps, totalEps, percentEps*100)

		if percentEps < cfg.SmartModeThreshold {
			return nil, domain.StatusBelowThreshold, domain.StatusBelowThreshold.Error()
		}
	}

	return slices.Dedupe(matches), domain.StatusSuccessfulMatch, nil
}

func (p *processor) ParseTorrentHandler(c *gin.Context) {
	p.log.Info().Msg("starting to parse season pack torrent")

	if err := json.NewDecoder(c.Request.Body).Decode(&p.req); err != nil {
		p.log.Error().Err(err).Msgf("%s", domain.StatusDecodingError)
		c.AbortWithStatusJSON(domain.StatusDecodingError.Code(), gin.H{
			"statusCode": domain.StatusDecodingError.Code(),
			"error":      err.Error(),
		})
		return
	}

	statusCode, err := p.parseTorrent()
	if err != nil {
		go func() {
			if sendErr := p.noti.Send(statusCode, domain.NotificationPayload{
				ReleaseName: p.req.Name,
				Client:      p.req.ClientName,
				Action:      "Parse",
				Error:       err,
			}); sendErr != nil {
				p.log.Error().Err(sendErr).Msgf("error sending %s notification for %d", p.noti.Name(), statusCode)
			}
		}()

		p.log.Error().Err(err).Msg("error parsing torrent")
		c.AbortWithStatusJSON(statusCode.Code(), gin.H{
			"statusCode": statusCode.Code(),
			"error":      err.Error(),
		})
		return
	}

	go func() {
		if sendErr := p.noti.Send(statusCode, domain.NotificationPayload{
			ReleaseName: p.req.Name,
			Client:      p.req.ClientName,
			Action:      "Parse",
		}); sendErr != nil {
			p.log.Error().Err(sendErr).Msgf("error sending %s notification for %d", p.noti.Name(), statusCode)
		}
	}()

	p.log.Info().Msg("successfully parsed torrent, hardlinked episodes, and imported the season pack")
	c.String(statusCode.Code(), statusCode.String())
}

// parseTorrent is the /api/parse path: it resolves the client's import root,
// recomputes the matches, hardlinks the matched episodes into the parsed pack
// folder, then imports the season pack back into the client.
func (p *processor) parseTorrent() (domain.StatusCode, error) {
	clientName := p.getClientName()
	snapshot := p.cfg.Snapshot()

	p.log.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Str("release", p.req.Name).Str("clientname", clientName)
	})

	clientCfg, ok := snapshot.Clients[clientName]
	if !ok {
		return domain.StatusClientNotFound, domain.StatusClientNotFound.Error()
	}
	p.log.Info().Msgf("using %s client", clientName)

	if len(p.req.Torrent) == 0 {
		return domain.StatusTorrentBytesError, domain.StatusTorrentBytesError.Error()
	}

	if err := p.getClient(clientCfg, clientName); err != nil {
		return domain.StatusGetClientError, fmt.Errorf("%s: %w", domain.StatusGetClientError, err)
	}

	importDestination, err := p.req.Client.ImportDestination()
	if err != nil {
		statusCode := torrentclient.ImportStatusCode(err)
		return statusCode, fmt.Errorf("%s: %w", statusCode, err)
	}
	importRoot := importDestination.SavePath()
	p.log.Debug().Msgf("resolved import root: %s", importRoot)

	matches, statusCode, err := p.collectMatches(clientName, clientCfg, snapshot)
	if err != nil {
		return statusCode, err
	}

	torrentBytes, err := torrents.DecodeTorrentBytes(p.req.Torrent)
	if err != nil {
		return domain.StatusDecodeTorrentBytesError, fmt.Errorf("%s: %w", domain.StatusDecodeTorrentBytesError, err)
	}
	p.req.Torrent = torrentBytes

	torrentInfo, err := torrents.Info(p.req.Torrent)
	if err != nil {
		return domain.StatusParseTorrentInfoError, fmt.Errorf("%s: %w", domain.StatusParseTorrentInfoError, err)
	}
	parsedPackName := torrentInfo.BestName()
	p.log.Debug().Msgf("parsed season pack name: %s", parsedPackName)

	torrentEps, err := torrents.Episodes(torrentInfo)
	if err != nil {
		return domain.StatusGetEpisodesError, fmt.Errorf("%s: %w", domain.StatusGetEpisodesError, err)
	}
	for _, torrentEp := range torrentEps {
		p.log.Debug().Msgf("found episode in pack: name(%s), size(%d)", torrentEp.Path, torrentEp.Size)
	}

	hashes, err := torrents.InfoHashes(p.req.Torrent)
	if err != nil {
		return domain.StatusParseTorrentInfoError, fmt.Errorf("%s: %w", domain.StatusParseTorrentInfoError, err)
	}

	successfulEpMatch := false
	linkedTargets := make(map[string]struct{})

	for _, match := range matches {
		matchedEpPath := ""
		var compareInfo domain.CompareInfo

		for _, torrentEp := range torrentEps {
			matchedEpPath, compareInfo = release.MatchEpToSeasonPackEp(match.clientEpPath, match.clientEpSize,
				torrentEp.Path, torrentEp.Size)
			if len(matchedEpPath) == 0 {
				p.log.Debug().Msgf("%s: client(%s => %v), torrent(%s => %v)", compareInfo.StatusCode,
					filepath.Base(match.clientEpPath), compareInfo.RejectValueA, torrentEp.Path, compareInfo.RejectValueB)
				continue
			}

			targetEpPath := importDestination.TargetPath(parsedPackName, matchedEpPath)
			successfulEpMatch = true

			// cross-seeded torrents can match the same target from different
			// sources; only hardlink each target once
			if _, linked := linkedTargets[targetEpPath]; linked {
				p.log.Debug().Msgf("skipping already linked target: %s", targetEpPath)
				break
			}

			if err = files.CreateHardlink(match.clientEpPath, targetEpPath); err != nil {
				p.log.Error().Err(err).Msgf("error creating hardlink: %s", match.clientEpPath)
				continue
			}
			p.log.Info().Msgf("created hardlink: source(%s), target(%s)", match.clientEpPath, targetEpPath)
			linkedTargets[targetEpPath] = struct{}{}

			break
		}

		if len(matchedEpPath) == 0 {
			p.log.Error().Msgf("error matching episode to file in pack, skipping hardlink: %s",
				filepath.Base(match.clientEpPath))
		}
	}

	p.log.Info().Msgf("hardlinked %d/%d episodes from pack", len(linkedTargets), len(torrentEps))

	if !successfulEpMatch {
		return domain.StatusFailedMatchToTorrentEps, domain.StatusFailedMatchToTorrentEps.Error()
	}

	if len(linkedTargets) == 0 {
		return domain.StatusFailedHardlink, domain.StatusFailedHardlink.Error()
	}

	if err := p.req.Client.Import(torrentclient.ImportRequest{
		TorrentBytes: p.req.Torrent,
		SavePath:     importRoot,
		LegacyHash:   hashes.Legacy,
		V2Hash:       hashes.V2,
		HasV1:        hashes.HasV1,
	}); err != nil {
		statusCode := torrentclient.ImportStatusCode(err)
		return statusCode, fmt.Errorf("%s: %w", statusCode, err)
	}

	return domain.StatusSuccessfulHardlink, nil
}
