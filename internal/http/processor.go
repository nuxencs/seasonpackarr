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

	"seasonpackarr/internal/config"
	"seasonpackarr/internal/domain"
	"seasonpackarr/internal/logger"
	"seasonpackarr/internal/release"
	"seasonpackarr/internal/torrents"
	"seasonpackarr/internal/utils"

	"github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/rs/zerolog"
)

const (
	StatusNoMatches                = 200
	StatusResolutionMismatch       = 201
	StatusSourceMismatch           = 202
	StatusRlsGrpMismatch           = 203
	StatusCutMismatch              = 204
	StatusEditionMismatch          = 205
	StatusRepackStatusMismatch     = 206
	StatusHdrMismatch              = 207
	StatusStreamingServiceMismatch = 208
	StatusAlreadyInClient          = 210
	StatusNotASeasonPack           = 211
	StatusBelowThreshold           = 230
	StatusSuccessfulMatch          = 250
	StatusSuccessfulHardlink       = 250
	StatusFailedHardlink           = 440
	StatusClientNotFound           = 472
	StatusGetClientError           = 471
	StatusDecodingError            = 470
	StatusAnnounceNameError        = 469
	StatusGetTorrentsError         = 468
	StatusTorrentBytesError        = 467
	StatusDecodeTorrentBytesError  = 466
	StatusParseTorrentInfoError    = 465
	StatusGetEpisodesError         = 464
	StatusEpisodeCountError        = 450
)

type processor struct {
	log zerolog.Logger
	cfg *config.AppConfig
	req *request
}

type request struct {
	Name       string
	Torrent    json.RawMessage
	Client     *qbittorrent.Client
	ClientName string
}

type entryTime struct {
	e   map[string][]domain.Entry
	d   map[string]rls.Release
	t   time.Time
	err error
	sync.Mutex
}

type matchPaths struct {
	epPathClient     string
	packPathAnnounce string
}

var (
	clientMap  sync.Map
	matchesMap sync.Map
	torrentMap sync.Map
)

func newProcessor(log logger.Logger, config *config.AppConfig) *processor {
	return &processor{
		log: log.With().Str("module", "processor").Logger(),
		cfg: config,
	}
}

func (p *processor) getClient(client *domain.Client) error {
	s := qbittorrent.Config{
		Host:     fmt.Sprintf("http://%s:%d", client.Host, client.Port),
		Username: client.Username,
		Password: client.Password,
	}

	c, ok := clientMap.Load(s)
	if !ok {
		c = qbittorrent.NewClient(s)

		if err := c.(*qbittorrent.Client).Login(); err != nil {
			p.log.Fatal().Err(err).Msg("error logging into qBittorrent")
		}

		clientMap.Store(s, c)
	}

	p.req.Client = c.(*qbittorrent.Client)
	return nil
}

func (p *processor) getAllTorrents(client *domain.Client) entryTime {
	set := qbittorrent.Config{
		Host:     fmt.Sprintf("http://%s:%d", client.Host, client.Port),
		Username: client.Username,
		Password: client.Password,
	}

	f := func() *entryTime {
		te, ok := torrentMap.Load(set)
		if ok {
			return te.(*entryTime)
		}

		res := &entryTime{d: make(map[string]rls.Release)}
		torrentMap.Store(set, res)
		return res
	}

	res := f()
	cur := time.Now()
	if res.t.After(cur) {
		return *res
	}

	res.Lock()
	defer res.Unlock()

	res = f()
	if res.t.After(cur) {
		return *res
	}

	ts, err := p.req.Client.GetTorrents(qbittorrent.TorrentFilterOptions{})
	if err != nil {
		return entryTime{err: err}
	}

	nt := time.Now()
	res = &entryTime{e: make(map[string][]domain.Entry), t: nt.Add(nt.Sub(cur)), d: res.d}

	for _, t := range ts {
		r, ok := res.d[t.Name]
		if !ok {
			r = rls.ParseString(t.Name)
			res.d[t.Name] = r
		}

		s := utils.GetFormattedTitle(r)
		res.e[s] = append(res.e[s], domain.Entry{T: t, R: r})
	}

	torrentMap.Store(set, res)
	return *res
}

func (p *processor) getFiles(hash string) (*qbittorrent.TorrentFiles, error) {
	return p.req.Client.GetFilesInformation(hash)
}

func (p *processor) ProcessSeasonPackHandler(w netHTTP.ResponseWriter, r *netHTTP.Request) {
	p.log.Info().Msg("starting to process season pack request")

	if err := json.NewDecoder(r.Body).Decode(&p.req); err != nil {
		p.log.Error().Err(err).Msgf("error decoding request")
		netHTTP.Error(w, err.Error(), StatusDecodingError)
		return
	}

	p.log = p.log.With().Str("release", p.req.Name).Logger()

	code, logMsg, err := p.processSeasonPack()
	if err != nil {
		p.log.Error().Err(err).Msgf("error processing season pack: %d", code)
		netHTTP.Error(w, err.Error(), code)
		return
	}

	p.log.Info().Msg(logMsg)
	w.WriteHeader(code)
}

func (p *processor) processSeasonPack() (int, string, error) {
	clientName := p.req.ClientName

	if len(clientName) == 0 {
		clientName = "default"
		p.req.ClientName = clientName
		p.log.Info().Msgf("no clientname defined. trying to use %q client", clientName)
	}

	client, ok := p.cfg.Config.Clients[clientName]
	if !ok {
		return StatusClientNotFound, "", fmt.Errorf("client not found in config: %q", clientName)
	}
	p.log.Info().Msgf("using %q client serving at %s:%d", clientName, client.Host, client.Port)

	if len(p.req.Name) == 0 {
		return StatusAnnounceNameError, "", fmt.Errorf("couldn't get announce name")
	}

	if err := p.getClient(client); err != nil {
		return StatusGetClientError, "", err
	}

	mp := p.getAllTorrents(client)
	if mp.err != nil {
		return StatusGetTorrentsError, "", mp.err
	}

	requestrls := domain.Entry{R: rls.ParseString(p.req.Name)}
	v, ok := mp.e[utils.GetFormattedTitle(requestrls.R)]
	if !ok {
		return StatusNoMatches, fmt.Sprintf("no matching releases in client %q", clientName), nil
	}

	packNameAnnounce := utils.FormatSeasonPackTitle(p.req.Name)
	p.log.Debug().Msgf("formatted season pack name: %q", packNameAnnounce)

	for _, child := range v {
		if release.CheckCandidates(&requestrls, &child, p.cfg.Config.FuzzyMatching) == StatusAlreadyInClient {
			return StatusAlreadyInClient, fmt.Sprintf("release already in client %q", clientName), nil
		}
	}

	var matchedEps []int
	var respCodes []int

	for _, child := range v {
		switch res := release.CheckCandidates(&requestrls, &child, p.cfg.Config.FuzzyMatching); res {
		case StatusResolutionMismatch:
			p.log.Info().Msgf("resolution did not match: request(%s => %s), client(%s => %s)",
				requestrls.R.String(), requestrls.R.Resolution, child.R.String(), child.R.Resolution)
			respCodes = append(respCodes, res)
			continue

		case StatusSourceMismatch:
			p.log.Info().Msgf("source did not match: request(%s => %s), client(%s => %s)",
				requestrls.R.String(), requestrls.R.Source, child.R.String(), child.R.Source)
			respCodes = append(respCodes, res)
			continue

		case StatusRlsGrpMismatch:
			p.log.Info().Msgf("release group did not match: request(%s => %s), client(%s => %s)",
				requestrls.R.String(), requestrls.R.Group, child.R.String(), child.R.Group)
			respCodes = append(respCodes, res)
			continue

		case StatusCutMismatch:
			p.log.Info().Msgf("cut did not match: request(%s => %s), client(%s => %s)",
				requestrls.R.String(), requestrls.R.Cut, child.R.String(), child.R.Cut)
			respCodes = append(respCodes, res)
			continue

		case StatusEditionMismatch:
			p.log.Info().Msgf("edition did not match: request(%s => %s), client(%s => %s)",
				requestrls.R.String(), requestrls.R.Edition, child.R.String(), child.R.Edition)
			respCodes = append(respCodes, res)
			continue

		case StatusRepackStatusMismatch:
			p.log.Info().Msgf("repack status did not match: request(%s => %s), client(%s => %s)",
				requestrls.R.String(), requestrls.R.Other, child.R.String(), child.R.Other)
			respCodes = append(respCodes, res)
			continue

		case StatusHdrMismatch:
			p.log.Info().Msgf("hdr metadata did not match: request(%s => %s), client(%s => %s)",
				requestrls.R.String(), requestrls.R.HDR, child.R.String(), child.R.HDR)
			respCodes = append(respCodes, res)
			continue

		case StatusStreamingServiceMismatch:
			p.log.Info().Msgf("streaming service did not match: request(%s => %s), client(%s => %s)",
				requestrls.R.String(), requestrls.R.Collection, child.R.String(), child.R.Collection)
			respCodes = append(respCodes, res)
			continue

		case StatusAlreadyInClient:
			return StatusAlreadyInClient, fmt.Sprintf("release already in client %q", clientName), nil

		case StatusNotASeasonPack:
			return StatusNotASeasonPack, fmt.Sprintf("release is not a season pack"), nil

		case StatusSuccessfulMatch:
			m, err := p.getFiles(child.T.Hash)
			if err != nil {
				p.log.Error().Err(err).Msgf("error getting files: %q", child.T.Name)
				continue
			}

			fileName := ""
			for _, v := range *m {
				fileName = v.Name
				break
			}

			epRls := rls.ParseString(child.T.Name)
			epPathClient := filepath.Join(child.T.SavePath, fileName)
			packPathAnnounce := filepath.Join(client.PreImportPath, packNameAnnounce, filepath.Base(fileName))

			matchedEps = append(matchedEps, epRls.Episode)

			currentMatch := []matchPaths{
				{
					epPathClient:     epPathClient,
					packPathAnnounce: packPathAnnounce,
				},
			}

			oldMatches, ok := matchesMap.Load(p.req.Name)
			if !ok {
				oldMatches = currentMatch
			}

			newMatches := append(oldMatches.([]matchPaths), currentMatch...)
			matchesMap.Store(p.req.Name, newMatches)
			p.log.Debug().Msgf("matched torrent from client %q: %q %q", clientName, child.T.Name, child.T.Hash)
			respCodes = append(respCodes, res)
			continue
		}
	}

	matchesSlice, ok := matchesMap.Load(p.req.Name)
	if !slices.Contains(respCodes, StatusSuccessfulMatch) || !ok {
		return StatusNoMatches, fmt.Sprintf("no matching releases in client %q", clientName), nil
	}

	if p.cfg.Config.SmartMode {
		reqRls := rls.ParseString(p.req.Name)

		totalEps, err := utils.GetEpisodesPerSeason(reqRls.Title, reqRls.Series)
		if err != nil {
			return StatusEpisodeCountError, "", err
		}
		matchedEps = utils.DedupeSlice(matchedEps)

		percentEps := release.PercentOfTotalEpisodes(totalEps, matchedEps)

		if percentEps < p.cfg.Config.SmartModeThreshold {
			// delete match from matchesMap if threshold is not met
			matchesMap.Delete(p.req.Name)

			return StatusBelowThreshold, fmt.Sprintf("found %d/%d (%.2f%%) episodes in client, below configured smart mode threshold",
				len(matchedEps), totalEps, percentEps*100), nil
		}
	}

	if p.cfg.Config.ParseTorrentFile {
		return StatusSuccessfulMatch, fmt.Sprintf("successfully matched season pack to episodes in client: %q", clientName), nil
	}

	matches := utils.DedupeSlice(matchesSlice.([]matchPaths))
	var hardlinkRespCodes []int

	for _, match := range matches {
		if err := utils.CreateHardlink(match.epPathClient, match.packPathAnnounce); err != nil {
			p.log.Error().Err(err).Msgf("error creating hardlink for: %q", match.epPathClient)
			hardlinkRespCodes = append(hardlinkRespCodes, StatusFailedHardlink)
			continue
		}
		p.log.Log().Msgf("created hardlink of %q into %q", match.epPathClient, match.packPathAnnounce)
		hardlinkRespCodes = append(hardlinkRespCodes, StatusSuccessfulHardlink)
	}

	if !slices.Contains(hardlinkRespCodes, StatusSuccessfulHardlink) {
		return StatusFailedHardlink, "", fmt.Errorf("couldn't create hardlinks")
	}
	return StatusSuccessfulHardlink, fmt.Sprintf("successfully created hardlinks for matched episodes in client: %q", clientName), nil
}

func (p *processor) ParseTorrentHandler(w netHTTP.ResponseWriter, r *netHTTP.Request) {
	p.log.Info().Msg("starting to parse season pack torrent")

	if err := json.NewDecoder(r.Body).Decode(&p.req); err != nil {
		p.log.Error().Err(err).Msgf("error decoding request")
		netHTTP.Error(w, err.Error(), StatusDecodingError)
		return
	}

	p.log = p.log.With().Str("release", p.req.Name).Logger()

	code, logMsg, err := p.parseTorrent()
	if err != nil {
		p.log.Error().Err(err).Msgf("error parsing torrent: %d", code)
		netHTTP.Error(w, err.Error(), code)
		return
	}

	p.log.Info().Msg(logMsg)
	w.WriteHeader(code)
}

func (p *processor) parseTorrent() (int, string, error) {
	if len(p.req.Name) == 0 {
		return StatusAnnounceNameError, "", fmt.Errorf("couldn't get announce name")
	}

	if len(p.req.Torrent) == 0 {
		return StatusTorrentBytesError, "", fmt.Errorf("couldn't get torrent bytes")
	}

	torrentBytes, err := torrents.DecodeTorrentDataRawBytes(p.req.Torrent)
	if err != nil {
		return StatusDecodeTorrentBytesError, "", err
	}
	p.req.Torrent = torrentBytes

	torrentInfo, err := torrents.ParseTorrentInfoFromTorrentBytes(p.req.Torrent)
	if err != nil {
		return StatusParseTorrentInfoError, "", err
	}
	packNameParsed := torrentInfo.BestName()
	p.log.Debug().Msgf("parsed season pack name: %q", packNameParsed)

	torrentEps, err := torrents.GetEpisodesFromTorrentInfo(torrentInfo)
	if err != nil {
		return StatusGetEpisodesError, "", err
	}
	for _, torrentEp := range torrentEps {
		p.log.Debug().Msgf("found episode in pack: %q", torrentEp)
	}

	matchesSlice, ok := matchesMap.Load(p.req.Name)
	if !ok {
		return StatusNoMatches, fmt.Sprintf("no matching releases in client"), nil
	}

	matches := utils.DedupeSlice(matchesSlice.([]matchPaths))
	var hardlinkRespCodes []int

	for _, match := range matches {
		newPackPath := utils.ReplaceParentFolder(match.packPathAnnounce, packNameParsed)
		newPackPath, err = utils.MatchFileNameToSeasonPackNaming(newPackPath, torrentEps)
		if err != nil {
			p.log.Error().Err(err).Msgf("error matching episode to file in season pack: %q", match.epPathClient)
		}

		if err = utils.CreateHardlink(match.epPathClient, newPackPath); err != nil {
			p.log.Error().Err(err).Msgf("error creating hardlink for: %q", match.epPathClient)
			hardlinkRespCodes = append(hardlinkRespCodes, StatusFailedHardlink)
			continue
		}
		p.log.Log().Msgf("created hardlink of %q into %q", match.epPathClient, newPackPath)
		hardlinkRespCodes = append(hardlinkRespCodes, StatusSuccessfulHardlink)
	}

	if !slices.Contains(hardlinkRespCodes, StatusSuccessfulHardlink) {
		return StatusFailedHardlink, "", fmt.Errorf("couldn't create hardlinks")
	}
	return StatusSuccessfulHardlink, "successfully parsed torrent and hardlinked episodes", nil
}
