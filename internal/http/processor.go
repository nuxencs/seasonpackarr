// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"encoding/json"
	"fmt"
	netHTTP "net/http"
	"path/filepath"
	"slices"
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

type matchPaths struct {
	clientEpPath    string
	clientEpSize    int64
	announcedEpPath string
}

var (
	clientMap  = xsync.NewMapOf[string, *qbittorrent.Client]()
	matchesMap = xsync.NewMapOf[string, []matchPaths]()
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

func (p *processor) ProcessSeasonPackHandler(w netHTTP.ResponseWriter, r *netHTTP.Request) {
	p.log.Info().Msg("starting to process season pack request")

	if err := json.NewDecoder(r.Body).Decode(&p.req); err != nil {
		p.log.Error().Err(err).Msgf("error decoding request")
		netHTTP.Error(w, err.Error(), domain.StatusDecodingError)
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
		netHTTP.Error(w, err.Error(), code)
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
	w.WriteHeader(code)
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

	requestEntry := domain.Entry{R: rls.ParseString(p.req.Name)}
	matchingEntries, ok := tre.entriesMap[utils.GetFormattedTitle(requestEntry.R)]
	if !ok {
		return domain.StatusNoMatches, fmt.Errorf("no matching releases in client")
	}

	announcedPackName := utils.FormatSeasonPackTitle(p.req.Name)
	p.log.Debug().Msgf("formatted season pack name: %s", announcedPackName)

	for _, entry := range matchingEntries {
		if release.CheckCandidates(&requestEntry, &entry, p.cfg.Config.FuzzyMatching) == domain.StatusAlreadyInClient {
			return domain.StatusAlreadyInClient, fmt.Errorf("release already in client")
		}
	}

	var matchedEps []int
	var respCodes []int

	for _, entry := range matchingEntries {
		switch res := release.CheckCandidates(&requestEntry, &entry, p.cfg.Config.FuzzyMatching); res {
		case domain.StatusResolutionMismatch:
			p.log.Info().Msgf("resolution did not match: request(%s => %s), client(%s => %s)",
				requestEntry.R.String(), requestEntry.R.Resolution, entry.R.String(), entry.R.Resolution)
			respCodes = append(respCodes, res)
			continue

		case domain.StatusSourceMismatch:
			p.log.Info().Msgf("source did not match: request(%s => %s), client(%s => %s)",
				requestEntry.R.String(), requestEntry.R.Source, entry.R.String(), entry.R.Source)
			respCodes = append(respCodes, res)
			continue

		case domain.StatusRlsGrpMismatch:
			p.log.Info().Msgf("release group did not match: request(%s => %s), client(%s => %s)",
				requestEntry.R.String(), requestEntry.R.Group, entry.R.String(), entry.R.Group)
			respCodes = append(respCodes, res)
			continue

		case domain.StatusCutMismatch:
			p.log.Info().Msgf("cut did not match: request(%s => %s), client(%s => %s)",
				requestEntry.R.String(), requestEntry.R.Cut, entry.R.String(), entry.R.Cut)
			respCodes = append(respCodes, res)
			continue

		case domain.StatusEditionMismatch:
			p.log.Info().Msgf("edition did not match: request(%s => %s), client(%s => %s)",
				requestEntry.R.String(), requestEntry.R.Edition, entry.R.String(), entry.R.Edition)
			respCodes = append(respCodes, res)
			continue

		case domain.StatusRepackStatusMismatch:
			p.log.Info().Msgf("repack status did not match: request(%s => %s), client(%s => %s)",
				requestEntry.R.String(), requestEntry.R.Other, entry.R.String(), entry.R.Other)
			respCodes = append(respCodes, res)
			continue

		case domain.StatusHdrMismatch:
			p.log.Info().Msgf("hdr metadata did not match: request(%s => %s), client(%s => %s)",
				requestEntry.R.String(), requestEntry.R.HDR, entry.R.String(), entry.R.HDR)
			respCodes = append(respCodes, res)
			continue

		case domain.StatusStreamingServiceMismatch:
			p.log.Info().Msgf("streaming service did not match: request(%s => %s), client(%s => %s)",
				requestEntry.R.String(), requestEntry.R.Collection, entry.R.String(), entry.R.Collection)
			respCodes = append(respCodes, res)
			continue

		case domain.StatusAlreadyInClient:
			return domain.StatusAlreadyInClient, fmt.Errorf("release already in client")

		case domain.StatusNotASeasonPack:
			return domain.StatusNotASeasonPack, fmt.Errorf("release is not a season pack")

		case domain.StatusSuccessfulMatch:
			torrentFiles, err := p.getFiles(entry.T.Hash)
			if err != nil {
				p.log.Error().Err(err).Msgf("error getting files: %s", entry.T.Name)
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
				p.log.Error().Err(err).Msgf("error getting filename or size: %s", entry.T.Name)
				continue
			}

			epRls := rls.ParseString(entry.T.Name)
			epPathClient := filepath.Join(entry.T.SavePath, fileName)
			announcedEpPath := filepath.Join(clientCfg.PreImportPath, announcedPackName, filepath.Base(fileName))

			matchedEps = append(matchedEps, epRls.Episode)

			currentMatch := []matchPaths{
				{
					clientEpPath:    epPathClient,
					clientEpSize:    size,
					announcedEpPath: announcedEpPath,
				},
			}

			oldMatches, ok := matchesMap.Load(p.req.Name)
			if !ok {
				oldMatches = currentMatch
			}

			newMatches := append(oldMatches, currentMatch...)
			matchesMap.Store(p.req.Name, newMatches)
			p.log.Debug().Msgf("matched torrent from client: name(%s), size(%d), hash(%s)",
				entry.T.Name, size, entry.T.Hash)
			respCodes = append(respCodes, res)
			continue
		}
	}

	matchesSlice, ok := matchesMap.Load(p.req.Name)
	if !slices.Contains(respCodes, domain.StatusSuccessfulMatch) || !ok {
		return domain.StatusNoMatches, fmt.Errorf("no matching releases in client")
	}

	if p.cfg.Config.SmartMode {
		reqRls := rls.ParseString(p.req.Name)

		totalEps, err := utils.GetEpisodesPerSeason(reqRls.Title, reqRls.Series)
		if err != nil {
			return domain.StatusEpisodeCountError, err
		}
		matchedEps = utils.DedupeSlice(matchedEps)

		percentEps := release.PercentOfTotalEpisodes(totalEps, matchedEps)

		if percentEps < p.cfg.Config.SmartModeThreshold {
			// delete match from matchesMap if threshold is not met
			matchesMap.Delete(p.req.Name)

			return domain.StatusBelowThreshold, fmt.Errorf("found %d/%d (%.2f%%) episodes in client, below configured smart mode threshold",
				len(matchedEps), totalEps, percentEps*100)
		}
	}

	if p.cfg.Config.ParseTorrentFile {
		return domain.StatusSuccessfulMatch, nil
	}

	matches := utils.DedupeSlice(matchesSlice)
	var hardlinkRespCodes []int

	for _, match := range matches {
		if err := utils.CreateHardlink(match.clientEpPath, match.announcedEpPath); err != nil {
			p.log.Error().Err(err).Msgf("error creating hardlink: %s", match.clientEpPath)
			hardlinkRespCodes = append(hardlinkRespCodes, domain.StatusFailedHardlink)
			continue
		}
		p.log.Log().Msgf("created hardlink: source(%s), target(%s)", match.clientEpPath, match.announcedEpPath)
		hardlinkRespCodes = append(hardlinkRespCodes, domain.StatusSuccessfulHardlink)
	}

	if !slices.Contains(hardlinkRespCodes, domain.StatusSuccessfulHardlink) {
		return domain.StatusFailedHardlink, fmt.Errorf("couldn't create hardlinks")
	}

	return domain.StatusSuccessfulHardlink, nil
}

func (p *processor) ParseTorrentHandler(w netHTTP.ResponseWriter, r *netHTTP.Request) {
	p.log.Info().Msg("starting to parse season pack torrent")

	if err := json.NewDecoder(r.Body).Decode(&p.req); err != nil {
		p.log.Error().Err(err).Msgf("error decoding request")
		netHTTP.Error(w, err.Error(), domain.StatusDecodingError)
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
		netHTTP.Error(w, err.Error(), code)
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
	w.WriteHeader(code)
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

	torrentBytes, err := torrents.DecodeTorrentDataRawBytes(p.req.Torrent)
	if err != nil {
		return domain.StatusDecodeTorrentBytesError, err
	}
	p.req.Torrent = torrentBytes

	torrentInfo, err := torrents.ParseTorrentInfoFromTorrentBytes(p.req.Torrent)
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

	matchesSlice, ok := matchesMap.Load(p.req.Name)
	if !ok {
		return domain.StatusNoMatches, fmt.Errorf("no matching releases in client")
	}

	matches := utils.DedupeSlice(matchesSlice)
	var hardlinkRespCodes []int
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
			hardlinkRespCodes = append(hardlinkRespCodes, domain.StatusFailedHardlink)
			continue
		}

		if err = utils.CreateHardlink(match.clientEpPath, targetEpPath); err != nil {
			p.log.Error().Err(err).Msgf("error creating hardlink: %s", match.clientEpPath)
			hardlinkRespCodes = append(hardlinkRespCodes, domain.StatusFailedHardlink)
			continue
		}
		p.log.Log().Msgf("created hardlink: source(%s), target(%s)", match.clientEpPath, targetEpPath)
		hardlinkRespCodes = append(hardlinkRespCodes, domain.StatusSuccessfulHardlink)
	}

	if !slices.Contains(hardlinkRespCodes, domain.StatusSuccessfulHardlink) {
		return domain.StatusFailedHardlink, fmt.Errorf("couldn't create hardlinks")
	}

	return domain.StatusSuccessfulHardlink, nil
}
