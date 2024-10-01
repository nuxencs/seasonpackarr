// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
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
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/release"
	"github.com/nuxencs/seasonpackarr/internal/torrents"
	"github.com/nuxencs/seasonpackarr/internal/utils"
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

type torrentRlsEntries struct {
	entriesMap  map[string][]domain.Entry
	rlsMap      map[string]rls.Release
	lastUpdated time.Time
	err         error
	sync.Mutex
}

type matchInfo struct {
	clientEpPath    string
	clientEpSize    int64
	announcedEpPath string
}

var (
	clientMap  = xsync.NewMapOf[string, *qbittorrent.Client]()
	matchesMap = xsync.NewMapOf[string, []matchInfo]()
	torrentMap = xsync.NewMapOf[string, *torrentRlsEntries]()
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

func (p *processor) getAllTorrents(clientName string) torrentRlsEntries {
	f := func() *torrentRlsEntries {
		tre, ok := torrentMap.Load(clientName)
		if ok {
			return tre
		}

		entries := &torrentRlsEntries{rlsMap: make(map[string]rls.Release)}
		torrentMap.Store(clientName, entries)
		return entries
	}

	entries := f()
	cur := time.Now()
	if entries.lastUpdated.After(cur) {
		return *entries
	}

	entries.Lock()
	defer entries.Unlock()

	entries = f()
	if entries.lastUpdated.After(cur) {
		return *entries
	}

	ts, err := p.req.Client.GetTorrents(qbittorrent.TorrentFilterOptions{})
	if err != nil {
		return torrentRlsEntries{err: err}
	}

	after := time.Now()
	entries = &torrentRlsEntries{entriesMap: make(map[string][]domain.Entry), lastUpdated: after.Add(after.Sub(cur)), rlsMap: entries.rlsMap}

	for _, t := range ts {
		r, ok := entries.rlsMap[t.Name]
		if !ok {
			r = rls.ParseString(t.Name)
			entries.rlsMap[t.Name] = r
		}

		fmtTitle := utils.GetFormattedTitle(r)
		entries.entriesMap[fmtTitle] = append(entries.entriesMap[fmtTitle], domain.Entry{T: t, R: r})
	}

	torrentMap.Store(clientName, entries)
	return *entries
}

func (p *processor) getFiles(hash string) (*qbittorrent.TorrentFiles, error) {
	return p.req.Client.GetFilesInformation(hash)
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
		p.log.Error().Err(err).Msgf("error decoding request")
		c.AbortWithStatusJSON(domain.StatusDecodingError, gin.H{"error": err.Error()})
		return
	}

	code, err := p.processSeasonPack()
	if err != nil {
		if sendErr := p.noti.Send(code, domain.NotificationPayload{
			ReleaseName: p.req.Name,
			Client:      p.req.ClientName,
			Action:      "Pack",
			Error:       err,
		}); sendErr != nil {
			p.log.Error().Err(sendErr).Msgf("could not send %s notification for %d", p.noti.Name(), code)
		}

		p.log.Error().Err(err).Msgf("error processing season pack: %d", code)
		c.AbortWithStatusJSON(code, gin.H{"error": err.Error()})
		return
	}

	if sendErr := p.noti.Send(code, domain.NotificationPayload{
		ReleaseName: p.req.Name,
		Client:      p.req.ClientName,
		Action:      "Pack",
	}); sendErr != nil {
		p.log.Error().Err(sendErr).Msgf("could not send %s notification for %d", p.noti.Name(), code)
	}

	p.log.Info().Msg("successfully matched season pack to episodes in client")
	c.String(code, "successful match")
}

func (p *processor) processSeasonPack() (int, error) {
	clientName := p.getClientName()

	p.log.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Str("release", p.req.Name).Str("clientname", clientName)
	})

	clientCfg, ok := p.cfg.Config.Clients[clientName]
	if !ok {
		return domain.StatusClientNotFound, fmt.Errorf("client not found in config")
	}
	p.log.Info().Msgf("using %s client serving at %s:%d", clientName, clientCfg.Host, clientCfg.Port)

	if len(p.req.Name) == 0 {
		return domain.StatusAnnounceNameError, fmt.Errorf("couldn't get announce name")
	}

	if err := p.getClient(clientCfg, clientName); err != nil {
		return domain.StatusGetClientError, err
	}

	tre := p.getAllTorrents(clientName)
	if tre.err != nil {
		return domain.StatusGetTorrentsError, tre.err
	}

	requestRls := rls.ParseString(p.req.Name)
	clientEntries, ok := tre.entriesMap[utils.GetFormattedTitle(requestRls)]
	if !ok {
		return domain.StatusNoMatches, fmt.Errorf("no matching releases in client")
	}

	announcedPackName := utils.FormatSeasonPackTitle(p.req.Name)
	p.log.Debug().Msgf("formatted season pack name: %s", announcedPackName)

	for _, clientEntry := range clientEntries {
		if release.CheckCandidates(requestRls, clientEntry.R, p.cfg.Config.FuzzyMatching) == domain.StatusAlreadyInClient {
			return domain.StatusAlreadyInClient, fmt.Errorf("release already in client")
		}
	}

	respSet := make(map[int]bool)
	epsSet := make(map[int]struct{})
	matches := make([]matchInfo, 0, len(clientEntries))

	for _, clientEntry := range clientEntries {
		switch res := release.CheckCandidates(requestRls, clientEntry.R, p.cfg.Config.FuzzyMatching); res {
		case domain.StatusResolutionMismatch:
			p.log.Info().Msgf("resolution did not match: request(%s => %s), client(%s => %s)",
				requestRls.String(), requestRls.Resolution, clientEntry.R.String(), clientEntry.R.Resolution)
			respSet[res] = true
			continue

		case domain.StatusSourceMismatch:
			p.log.Info().Msgf("source did not match: request(%s => %s), client(%s => %s)",
				requestRls.String(), requestRls.Source, clientEntry.R.String(), clientEntry.R.Source)
			respSet[res] = true
			continue

		case domain.StatusRlsGrpMismatch:
			p.log.Info().Msgf("release group did not match: request(%s => %s), client(%s => %s)",
				requestRls.String(), requestRls.Group, clientEntry.R.String(), clientEntry.R.Group)
			respSet[res] = true
			continue

		case domain.StatusCutMismatch:
			p.log.Info().Msgf("cut did not match: request(%s => %s), client(%s => %s)",
				requestRls.String(), requestRls.Cut, clientEntry.R.String(), clientEntry.R.Cut)
			respSet[res] = true
			continue

		case domain.StatusEditionMismatch:
			p.log.Info().Msgf("edition did not match: request(%s => %s), client(%s => %s)",
				requestRls.String(), requestRls.Edition, clientEntry.R.String(), clientEntry.R.Edition)
			respSet[res] = true
			continue

		case domain.StatusRepackStatusMismatch:
			p.log.Info().Msgf("repack status did not match: request(%s => %s), client(%s => %s)",
				requestRls.String(), requestRls.Other, clientEntry.R.String(), clientEntry.R.Other)
			respSet[res] = true
			continue

		case domain.StatusHdrMismatch:
			p.log.Info().Msgf("hdr metadata did not match: request(%s => %s), client(%s => %s)",
				requestRls.String(), requestRls.HDR, clientEntry.R.String(), clientEntry.R.HDR)
			respSet[res] = true
			continue

		case domain.StatusStreamingServiceMismatch:
			p.log.Info().Msgf("streaming service did not match: request(%s => %s), client(%s => %s)",
				requestRls.String(), requestRls.Collection, clientEntry.R.String(), clientEntry.R.Collection)
			respSet[res] = true
			continue

		case domain.StatusAlreadyInClient:
			return domain.StatusAlreadyInClient, fmt.Errorf("release already in client")

		case domain.StatusNotASeasonPack:
			return domain.StatusNotASeasonPack, fmt.Errorf("release is not a season pack")

		case domain.StatusSuccessfulMatch:
			torrentFiles, err := p.getFiles(clientEntry.T.Hash)
			if err != nil {
				p.log.Error().Err(err).Msgf("error getting files: %s", clientEntry.T.Name)
				continue
			}

			var fileName = ""
			var size int64 = 0
			for _, f := range *torrentFiles {
				if filepath.Ext(f.Name) != ".mkv" {
					continue
				}

				fileName = f.Name
				size = f.Size
				break
			}
			if len(fileName) == 0 || size == 0 {
				p.log.Error().Err(err).Msgf("error getting filename or size: %s", clientEntry.T.Name)
				continue
			}

			epRls := rls.ParseString(clientEntry.T.Name)
			clientEpPath := filepath.Join(clientEntry.T.SavePath, fileName)
			announcedEpPath := filepath.Join(clientCfg.PreImportPath, announcedPackName, filepath.Base(fileName))

			epsSet[epRls.Episode] = struct{}{}

			// append current matchInfo to matches slice
			matches = append(matches, matchInfo{
				clientEpPath:    clientEpPath,
				clientEpSize:    size,
				announcedEpPath: announcedEpPath,
			})

			p.log.Debug().Msgf("matched torrent from client: name(%s), size(%d), hash(%s)",
				clientEntry.T.Name, size, clientEntry.T.Hash)
			respSet[res] = true
			continue
		}
	}

	if !respSet[domain.StatusSuccessfulMatch] {
		return domain.StatusNoMatches, fmt.Errorf("no matching releases in client")
	}

	// dedupe matches and store in matchesMap
	matches = utils.DedupeSlice(matches)
	matchesMap.Store(p.req.Name, matches)

	if p.cfg.Config.SmartMode {
		totalEps, err := utils.GetEpisodesPerSeason(requestRls.Title, requestRls.Series)
		if err != nil {
			return domain.StatusEpisodeCountError, err
		}

		foundEps := len(epsSet)
		percentEps := release.PercentOfTotalEpisodes(totalEps, foundEps)

		if percentEps < p.cfg.Config.SmartModeThreshold {
			// delete match from matchesMap if threshold is not met
			matchesMap.Delete(p.req.Name)

			return domain.StatusBelowThreshold, fmt.Errorf("found %d/%d (%.2f%%) episodes in client, below configured smart mode threshold",
				foundEps, totalEps, percentEps*100)
		}
	}

	if p.cfg.Config.ParseTorrentFile {
		return domain.StatusSuccessfulMatch, nil
	}

	hardlinkRespSet := make(map[int]bool)

	for _, match := range matches {
		if err := utils.CreateHardlink(match.clientEpPath, match.announcedEpPath); err != nil {
			p.log.Error().Err(err).Msgf("error creating hardlink: %s", match.clientEpPath)
			hardlinkRespSet[domain.StatusFailedHardlink] = true
			continue
		}
		p.log.Log().Msgf("created hardlink: source(%s), target(%s)", match.clientEpPath, match.announcedEpPath)
		hardlinkRespSet[domain.StatusSuccessfulHardlink] = true
	}

	if !hardlinkRespSet[domain.StatusSuccessfulHardlink] {
		return domain.StatusFailedHardlink, fmt.Errorf("couldn't create hardlinks")
	}

	return domain.StatusSuccessfulHardlink, nil
}

func (p *processor) ParseTorrentHandler(c *gin.Context) {
	p.log.Info().Msg("starting to parse season pack torrent")

	if err := json.NewDecoder(c.Request.Body).Decode(&p.req); err != nil {
		p.log.Error().Err(err).Msgf("error decoding request")
		c.AbortWithStatusJSON(domain.StatusDecodingError, gin.H{"error": err.Error()})
		return
	}

	code, err := p.parseTorrent()
	if err != nil {
		if sendErr := p.noti.Send(code, domain.NotificationPayload{
			ReleaseName: p.req.Name,
			Client:      p.req.ClientName,
			Action:      "Parse",
			Error:       err,
		}); sendErr != nil {
			p.log.Error().Err(sendErr).Msgf("could not send %s notification for %d", p.noti.Name(), code)
		}

		p.log.Error().Err(err).Msgf("error parsing torrent: %d", code)
		c.AbortWithStatusJSON(code, gin.H{"error": err.Error()})
		return
	}

	if sendErr := p.noti.Send(code, domain.NotificationPayload{
		ReleaseName: p.req.Name,
		Client:      p.req.ClientName,
		Action:      "Parse",
	}); sendErr != nil {
		p.log.Error().Err(sendErr).Msgf("could not send %s notification for %d", p.noti.Name(), code)
	}

	p.log.Info().Msg("successfully parsed torrent and hardlinked episodes")
	c.String(code, "successful hardlink")
}

func (p *processor) parseTorrent() (int, error) {
	clientName := p.getClientName()

	p.log.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Str("release", p.req.Name).Str("clientname", clientName)
	})

	clientCfg, ok := p.cfg.Config.Clients[clientName]
	if !ok {
		return domain.StatusClientNotFound, fmt.Errorf("client not found in config")
	}

	if len(p.req.Name) == 0 {
		return domain.StatusAnnounceNameError, fmt.Errorf("couldn't get announce name")
	}

	if len(p.req.Torrent) == 0 {
		return domain.StatusTorrentBytesError, fmt.Errorf("couldn't get torrent bytes")
	}

	torrentBytes, err := torrents.DecodeTorrentBytes(p.req.Torrent)
	if err != nil {
		return domain.StatusDecodeTorrentBytesError, err
	}
	p.req.Torrent = torrentBytes

	torrentInfo, err := torrents.ParseInfoFromTorrentBytes(p.req.Torrent)
	if err != nil {
		return domain.StatusParseTorrentInfoError, err
	}
	parsedPackName := torrentInfo.BestName()
	p.log.Debug().Msgf("parsed season pack name: %s", parsedPackName)

	torrentEps, err := torrents.GetEpisodesFromTorrentInfo(torrentInfo)
	if err != nil {
		return domain.StatusGetEpisodesError, err
	}
	for _, torrentEp := range torrentEps {
		p.log.Debug().Msgf("found episode in pack: name(%s), size(%d)", torrentEp.Path, torrentEp.Size)
	}

	matches, ok := matchesMap.Load(p.req.Name)
	if !ok {
		return domain.StatusNoMatches, fmt.Errorf("no matching releases in client")
	}

	hardlinkRespSet := make(map[int]bool)

	var matchedEpPath string
	var matchErr error
	var targetEpPath string

	targetPackDir := filepath.Join(clientCfg.PreImportPath, parsedPackName)

	for _, match := range matches {
		for _, torrentEp := range torrentEps {
			// reset targetEpPath for each checked torrentEp
			targetEpPath = ""

			matchedEpPath, matchErr = utils.MatchEpToSeasonPackEp(match.clientEpPath, match.clientEpSize,
				torrentEp.Path, torrentEp.Size)
			if matchErr != nil {
				p.log.Debug().Err(matchErr).Msgf("episode did not match: client(%s), torrent(%s)",
					filepath.Base(match.clientEpPath), torrentEp.Path)
				continue
			}
			targetEpPath = filepath.Join(targetPackDir, matchedEpPath)
			break
		}
		if matchErr != nil {
			p.log.Error().Err(matchErr).Msgf("error matching episode to file in pack, skipping hardlink: %s",
				filepath.Base(match.clientEpPath))
			hardlinkRespSet[domain.StatusFailedHardlink] = true
			continue
		}

		if err = utils.CreateHardlink(match.clientEpPath, targetEpPath); err != nil {
			p.log.Error().Err(err).Msgf("error creating hardlink: %s", match.clientEpPath)
			hardlinkRespSet[domain.StatusFailedHardlink] = true
			continue
		}
		p.log.Log().Msgf("created hardlink: source(%s), target(%s)", match.clientEpPath, targetEpPath)
		hardlinkRespSet[domain.StatusSuccessfulHardlink] = true
	}

	if !hardlinkRespSet[domain.StatusSuccessfulHardlink] {
		return domain.StatusFailedHardlink, fmt.Errorf("couldn't create hardlinks")
	}

	return domain.StatusSuccessfulHardlink, nil
}
