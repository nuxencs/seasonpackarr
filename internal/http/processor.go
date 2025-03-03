// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"encoding/json"
	"fmt"
	"path/filepath"
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
	"github.com/nuxencs/seasonpackarr/internal/torrents"
	"github.com/nuxencs/seasonpackarr/pkg/errors"

	"github.com/autobrr/go-qbittorrent"
	"github.com/gin-gonic/gin"
	"github.com/moistari/rls"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/rs/zerolog"
)

type processor struct {
	log  zerolog.Logger
	cfg  *config.AppConfig
	noti domain.Sender
	req  *request
}

type request struct {
	Name       string
	Torrent    json.RawMessage
	Client     *qbittorrent.Client
	ClientName string
}

type entry struct {
	torrent qbittorrent.Torrent
	release rls.Release
}

type entryCache struct {
	entriesMap  map[string][]entry
	rlsMap      map[string]rls.Release
	lastUpdated time.Time
	mu          sync.Mutex
}

type matchInfo struct {
	clientEpPath    string
	clientEpSize    int64
	announcedEpPath string
}

var (
	clientMap = xsync.NewMapOf[string, *qbittorrent.Client]()
	matchMap  = xsync.NewMapOf[string, []matchInfo]()
	entryMap  = xsync.NewMapOf[string, *entryCache]()
)

func newProcessor(log logger.Logger, config *config.AppConfig, notification domain.Sender) *processor {
	return &processor{
		log:  log.With().Str("module", "processor").Logger(),
		cfg:  config,
		noti: notification,
	}
}

func (p *processor) getClient(client *domain.Client, clientName string) error {
	c, ok := clientMap.Load(clientName)
	if !ok {
		clientCfg := qbittorrent.Config{
			Host:     fmt.Sprintf("http://%s:%d", client.Host, client.Port),
			Username: client.Username,
			Password: client.Password,
		}

		c = qbittorrent.NewClient(clientCfg)

		if err := c.Login(); err != nil {
			return errors.Wrap(err, "failed to login to qbittorrent")
		}

		clientMap.Store(clientName, c)
	}

	p.req.Client = c
	return nil
}

func (p *processor) getAllTorrents(clientName string) (map[string][]entry, error) {
	f := func() *entryCache {
		tre, ok := entryMap.Load(clientName)
		if ok {
			return tre
		}

		entries := &entryCache{rlsMap: make(map[string]rls.Release)}
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

	entries = f()
	if entries.lastUpdated.After(cur) {
		return entries.entriesMap, nil
	}

	ts, err := p.req.Client.GetTorrents(qbittorrent.TorrentFilterOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get torrents")
	}

	after := time.Now()
	entries = &entryCache{entriesMap: make(map[string][]entry), lastUpdated: after.Add(after.Sub(cur)), rlsMap: entries.rlsMap}

	for _, t := range ts {
		r, ok := entries.rlsMap[t.Name]
		if !ok {
			r = rls.ParseString(t.Name)
			entries.rlsMap[t.Name] = r
		}

		comparableTitle := format.ComparableTitle(r)
		entries.entriesMap[comparableTitle] = append(entries.entriesMap[comparableTitle], entry{torrent: t, release: r})
	}

	entryMap.Store(clientName, entries)
	return entries.entriesMap, nil
}

func (p *processor) getFiles(hash string) (string, int64, error) {
	torrentFiles, err := p.req.Client.GetFilesInformation(hash)
	if err != nil {
		return "", 0, err
	}

	var fileName string
	var size int64
	for _, f := range *torrentFiles {
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

func (p *processor) processSeasonPack() (domain.StatusCode, error) {
	clientName := p.getClientName()

	p.log.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Str("release", p.req.Name).Str("clientname", clientName)
	})

	if len(p.req.Name) == 0 {
		return domain.StatusAnnounceNameError, domain.StatusAnnounceNameError.Error()
	}

	clientCfg, ok := p.cfg.Config.Clients[clientName]
	if !ok {
		return domain.StatusClientNotFound, domain.StatusClientNotFound.Error()
	}
	p.log.Info().Msgf("using %s client serving at %s:%d", clientName, clientCfg.Host, clientCfg.Port)

	if err := p.getClient(clientCfg, clientName); err != nil {
		return domain.StatusGetClientError, errors.Wrap(err, domain.StatusGetClientError.String())
	}

	entries, err := p.getAllTorrents(clientName)
	if err != nil {
		return domain.StatusGetTorrentsError, errors.Wrap(err, domain.StatusGetTorrentsError.String())
	}

	requestRls := rls.ParseString(p.req.Name)
	filteredEntries, ok := entries[format.ComparableTitle(requestRls)]
	if !ok {
		return domain.StatusNoMatches, domain.StatusNoMatches.Error()
	}

	announcedPackName := format.CleanAnnounceTitle(requestRls)
	p.log.Debug().Msgf("formatted season pack name: %s", announcedPackName)

	for _, filteredEntry := range filteredEntries {
		switch compareInfo := release.CheckCandidates(requestRls, filteredEntry.release, p.cfg.Config.FuzzyMatching); compareInfo.StatusCode {
		case domain.StatusAlreadyInClient, domain.StatusNotASeasonPack:
			return compareInfo.StatusCode, compareInfo.StatusCode.Error()
		}
	}

	codeSet := make(map[domain.StatusCode]bool)
	epsSet := make(map[int]struct{})
	matches := make([]matchInfo, 0, len(filteredEntries))

	for _, filteredEntry := range filteredEntries {
		switch compareInfo := release.CheckCandidates(requestRls, filteredEntry.release, p.cfg.Config.FuzzyMatching); compareInfo.StatusCode {
		case domain.StatusAlreadyInClient, domain.StatusNotASeasonPack:
			return compareInfo.StatusCode, compareInfo.StatusCode.Error()

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
			announcedEpPath := filepath.Join(clientCfg.PreImportPath, announcedPackName, filepath.Base(fileName))

			epsSet[filteredEntry.release.Episode] = struct{}{}

			// append current matchInfo to matches slice
			matches = append(matches, matchInfo{
				clientEpPath:    clientEpPath,
				clientEpSize:    size,
				announcedEpPath: announcedEpPath,
			})

			p.log.Debug().Msgf("matched torrent from client: name(%s), size(%d), hash(%s)",
				filteredEntry.torrent.Name, size, filteredEntry.torrent.Hash)
			codeSet[compareInfo.StatusCode] = true
			continue
		}
	}

	if !codeSet[domain.StatusSuccessfulMatch] {
		return domain.StatusNoMatches, domain.StatusNoMatches.Error()
	}

	if p.cfg.Config.SmartMode {
		totalEps, err := metadata.EpisodesInSeason(requestRls)
		if err != nil {
			return domain.StatusEpisodeCountError, errors.Wrap(err, domain.StatusEpisodeCountError.String())
		}

		foundEps := len(epsSet)
		percentEps := release.PercentOfTotalEpisodes(totalEps, foundEps)

		if percentEps < p.cfg.Config.SmartModeThreshold {
			return domain.StatusBelowThreshold, errors.Wrap(fmt.Errorf("found %d/%d (%.2f%%) episodes in client",
				foundEps, totalEps, percentEps*100), domain.StatusBelowThreshold.String())
		}
	}

	// dedupe matches and store in matchMap
	matches = slices.Dedupe(matches)
	matchMap.Store(p.req.Name, matches)

	if p.cfg.Config.ParseTorrentFile {
		return domain.StatusSuccessfulMatch, nil
	}

	successfulHardlink := false

	for _, match := range matches {
		if err := files.CreateHardlink(match.clientEpPath, match.announcedEpPath); err != nil {
			p.log.Error().Err(err).Msgf("error creating hardlink: %s", match.clientEpPath)
			continue
		}
		p.log.Log().Msgf("created hardlink: source(%s), target(%s)", match.clientEpPath, match.announcedEpPath)
		successfulHardlink = true
	}

	if !successfulHardlink {
		return domain.StatusFailedHardlink, domain.StatusFailedHardlink.Error()
	}

	return domain.StatusSuccessfulHardlink, nil
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

	p.log.Info().Msg("successfully parsed torrent and hardlinked episodes")
	c.String(statusCode.Code(), statusCode.String())
}

func (p *processor) parseTorrent() (domain.StatusCode, error) {
	clientName := p.getClientName()

	p.log.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Str("release", p.req.Name).Str("clientname", clientName)
	})

	clientCfg, ok := p.cfg.Config.Clients[clientName]
	if !ok {
		return domain.StatusClientNotFound, domain.StatusClientNotFound.Error()
	}

	if len(p.req.Name) == 0 {
		return domain.StatusAnnounceNameError, domain.StatusAnnounceNameError.Error()
	}

	if len(p.req.Torrent) == 0 {
		return domain.StatusTorrentBytesError, domain.StatusTorrentBytesError.Error()
	}

	torrentBytes, err := torrents.DecodeTorrentBytes(p.req.Torrent)
	if err != nil {
		return domain.StatusDecodeTorrentBytesError, errors.Wrap(err, domain.StatusDecodeTorrentBytesError.String())
	}
	p.req.Torrent = torrentBytes

	torrentInfo, err := torrents.Info(p.req.Torrent)
	if err != nil {
		return domain.StatusParseTorrentInfoError, errors.Wrap(err, domain.StatusParseTorrentInfoError.String())
	}
	parsedPackName := torrentInfo.BestName()
	p.log.Debug().Msgf("parsed season pack name: %s", parsedPackName)

	torrentEps, err := torrents.Episodes(torrentInfo)
	if err != nil {
		return domain.StatusGetEpisodesError, errors.Wrap(err, domain.StatusGetEpisodesError.String())
	}
	for _, torrentEp := range torrentEps {
		p.log.Debug().Msgf("found episode in pack: name(%s), size(%d)", torrentEp.Path, torrentEp.Size)
	}

	matches, ok := matchMap.Load(p.req.Name)
	if !ok {
		return domain.StatusNoMatches, domain.StatusNoMatches.Error()
	}

	successfulEpMatch := false
	successfulHardlink := false

	var matchedEpPath string
	var compareInfo domain.CompareInfo

	targetPackDir := filepath.Join(clientCfg.PreImportPath, parsedPackName)

	for _, match := range matches {
		for _, torrentEp := range torrentEps {
			var targetEpPath string

			matchedEpPath, compareInfo = release.MatchEpToSeasonPackEp(match.clientEpPath, match.clientEpSize,
				torrentEp.Path, torrentEp.Size)
			if len(matchedEpPath) == 0 {
				p.log.Debug().Msgf("%s: client(%s => %v), torrent(%s => %v)", compareInfo.StatusCode,
					filepath.Base(match.clientEpPath), compareInfo.RejectValueA, torrentEp.Path, compareInfo.RejectValueB)
				continue
			}
			targetEpPath = filepath.Join(targetPackDir, matchedEpPath)
			successfulEpMatch = true

			if err = files.CreateHardlink(match.clientEpPath, targetEpPath); err != nil {
				p.log.Error().Err(err).Msgf("error creating hardlink: %s", match.clientEpPath)
				continue
			}
			p.log.Log().Msgf("created hardlink: source(%s), target(%s)", match.clientEpPath, targetEpPath)
			successfulHardlink = true

			break
		}
		if len(matchedEpPath) == 0 {
			p.log.Error().Msgf("error matching episode to file in pack, skipping hardlink: %s",
				filepath.Base(match.clientEpPath))
			continue
		}
	}

	if !successfulEpMatch {
		return domain.StatusFailedMatchToTorrentEps, domain.StatusFailedMatchToTorrentEps.Error()
	}

	if !successfulHardlink {
		return domain.StatusFailedHardlink, domain.StatusFailedHardlink.Error()
	}

	return domain.StatusSuccessfulHardlink, nil
}
