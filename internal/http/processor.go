// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
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

type qbitClient interface {
	GetTorrents(qbittorrent.TorrentFilterOptions) ([]qbittorrent.Torrent, error)
	GetFilesInformation(hash string) (*qbittorrent.TorrentFiles, error)
	AddTorrentFromMemory(buf []byte, options map[string]string) error
	GetCategories() (map[string]qbittorrent.Category, error)
	GetDefaultSavePath() (string, error)
	Recheck(hashes []string) error
	Resume(hashes []string) error
}

type processor struct {
	log  zerolog.Logger
	cfg  *config.AppConfig
	noti domain.Sender
	meta *metadata.Provider
	req  *request
}

type request struct {
	Name       string
	Torrent    json.RawMessage
	Client     qbitClient
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
	clientEpPath string
	clientEpSize int64
}

var (
	clientMap = xsync.NewMapOf[string, qbitClient]()
	entryMap  = xsync.NewMapOf[string, *entryCache]()
)

func newProcessor(log logger.Logger, config *config.AppConfig, notification domain.Sender, metadata *metadata.Provider) *processor {
	return &processor{
		log:  log.With().Str("module", "processor").Logger(),
		cfg:  config,
		noti: notification,
		meta: metadata,
	}
}

func (p *processor) getClient(client *domain.Client, clientName string) error {
	if p.req.Client != nil {
		return nil
	}

	c, ok := clientMap.Load(clientName)
	if !ok {
		host, err := buildHost(client)
		if err != nil {
			return errors.Wrap(err, "failed to build host")
		}

		clientCfg := qbittorrent.Config{
			Host:     host,
			Username: client.Username,
			Password: client.Password,
			APIKey:   client.APIKey,
		}

		rawClient := qbittorrent.NewClient(clientCfg)

		if err := rawClient.Login(); err != nil {
			return errors.Wrap(err, "failed to login to qbittorrent")
		}

		c = rawClient
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

		comparableTitle := format.ComparableTitle(r, p.cfg.Config.FuzzyMatching)
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

	clientCfg, ok := p.cfg.Config.Clients[clientName]
	if !ok {
		return domain.StatusClientNotFound, domain.StatusClientNotFound.Error()
	}
	p.log.Info().Msgf("using %s client", clientName)

	if _, statusCode, err := p.collectMatches(clientName, clientCfg); err != nil {
		return statusCode, err
	}

	return domain.StatusSuccessfulMatch, nil
}

func (p *processor) collectMatches(clientName string, clientCfg *domain.Client) ([]matchInfo, domain.StatusCode, error) {
	if len(p.req.Name) == 0 {
		return nil, domain.StatusAnnounceNameError, domain.StatusAnnounceNameError.Error()
	}

	if err := p.getClient(clientCfg, clientName); err != nil {
		return nil, domain.StatusGetClientError, fmt.Errorf("%s: %w", domain.StatusGetClientError, err)
	}

	entries, err := p.getAllTorrents(clientName)
	if err != nil {
		return nil, domain.StatusGetTorrentsError, fmt.Errorf("%s: %w", domain.StatusGetTorrentsError, err)
	}

	requestRls := rls.ParseString(p.req.Name)
	filteredEntries, ok := entries[format.ComparableTitle(requestRls, p.cfg.Config.FuzzyMatching)]
	if !ok {
		return nil, domain.StatusNoMatches, domain.StatusNoMatches.Error()
	}

	for _, filteredEntry := range filteredEntries {
		switch compareInfo := release.CheckCandidates(requestRls, filteredEntry.release, p.cfg.Config.FuzzyMatching); compareInfo.StatusCode {
		case domain.StatusAlreadyInClient, domain.StatusNotASeasonPack:
			return nil, compareInfo.StatusCode, compareInfo.StatusCode.Error()
		}
	}

	codeSet := make(map[domain.StatusCode]bool)
	epsSet := make(map[int]struct{})
	matches := make([]matchInfo, 0, len(filteredEntries))

	for _, filteredEntry := range filteredEntries {
		switch compareInfo := release.CheckCandidates(requestRls, filteredEntry.release, p.cfg.Config.FuzzyMatching); compareInfo.StatusCode {
		case domain.StatusAlreadyInClient, domain.StatusNotASeasonPack:
			return nil, compareInfo.StatusCode, compareInfo.StatusCode.Error()

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

	if p.cfg.Config.SmartMode {
		totalEps, err := p.meta.EpisodesInSeason(requestRls)
		if err != nil {
			return nil, domain.StatusEpisodeCountError, fmt.Errorf("%s: %w", domain.StatusEpisodeCountError, err)
		}

		foundEps := len(epsSet)
		percentEps := release.PercentOfTotalEpisodes(totalEps, foundEps)

		p.log.Info().Msgf("found %d/%d (%.2f%%) episodes in client", foundEps, totalEps, percentEps*100)

		if percentEps < p.cfg.Config.SmartModeThreshold {
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

func (p *processor) parseTorrent() (domain.StatusCode, error) {
	clientName := p.getClientName()

	p.log.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Str("release", p.req.Name).Str("clientname", clientName)
	})

	clientCfg, ok := p.cfg.Config.Clients[clientName]
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

	statusCode, err := p.validateImportDestination(clientCfg)
	if err != nil {
		return statusCode, err
	}

	matches, statusCode, err := p.collectMatches(clientName, clientCfg)
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

	successfulEpMatch := false
	targetPackDir := filepath.Join(clientCfg.PreImportPath, parsedPackName)
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

			targetEpPath := filepath.Join(targetPackDir, matchedEpPath)
			successfulEpMatch = true

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

	statusCode, err = p.importSeasonPack(clientCfg, torrentBytes)
	if err != nil {
		return statusCode, err
	}

	return domain.StatusSuccessfulHardlink, nil
}

func (p *processor) importSeasonPack(clientCfg *domain.Client, torrentBytes []byte) (domain.StatusCode, error) {
	hash, err := torrents.InfoHash(torrentBytes)
	if err != nil {
		return domain.StatusParseTorrentInfoError, fmt.Errorf("%s: %w", domain.StatusParseTorrentInfoError, err)
	}

	options, statusCode, err := buildTorrentAddOptions(clientCfg)
	if err != nil {
		return statusCode, err
	}

	if err := p.req.Client.AddTorrentFromMemory(torrentBytes, options.Prepare()); err != nil {
		return domain.StatusAddTorrentError, fmt.Errorf("%s: %w", domain.StatusAddTorrentError, err)
	}
	p.log.Info().Msgf("added torrent to qbittorrent: hash(%s), savepath(%s), category(%s)",
		hash, options.SavePath, options.Category)

	addedTorrent, statusCode, err := p.waitForTorrent(hash, 15*time.Second)
	if err != nil {
		return statusCode, err
	}

	if addedTorrent.State == qbittorrent.TorrentStateMissingFiles {
		p.log.Info().Msgf("torrent entered missingFiles state, triggering recheck: hash(%s)", addedTorrent.Hash)
		if err := p.req.Client.Recheck([]string{addedTorrent.Hash}); err != nil {
			return domain.StatusRecheckTorrentError, fmt.Errorf("%s: %w", domain.StatusRecheckTorrentError, err)
		}

		addedTorrent, statusCode, err = p.waitForRecheck(addedTorrent.Hash, 1*time.Minute)
		if err != nil {
			return statusCode, err
		}
	}

	if isActiveTorrentState(addedTorrent.State) {
		return domain.StatusSuccessfulHardlink, nil
	}

	if err := p.req.Client.Resume([]string{addedTorrent.Hash}); err != nil {
		return domain.StatusResumeTorrentError, fmt.Errorf("%s: %w", domain.StatusResumeTorrentError, err)
	}
	p.log.Info().Msgf("resumed imported torrent: hash(%s), state(%s)", addedTorrent.Hash, addedTorrent.State)

	return domain.StatusSuccessfulHardlink, nil
}

func buildTorrentAddOptions(clientCfg *domain.Client) (*qbittorrent.TorrentAddOptions, domain.StatusCode, error) {
	contentLayout, ok, err := resolveContentLayout(clientCfg.Qbit.ContentLayout)
	if err != nil {
		return nil, domain.StatusQbitConfigError, fmt.Errorf("%s: %w", domain.StatusQbitConfigError, err)
	}

	opts := &qbittorrent.TorrentAddOptions{
		SkipHashCheck: true,
		Paused:        clientCfg.Qbit.PausedOnAdd,
	}

	if clientCfg.Qbit.Category != "" {
		opts.Category = strings.TrimSpace(clientCfg.Qbit.Category)
	}
	if clientCfg.Qbit.SavePath != "" {
		opts.SavePath = strings.TrimSpace(clientCfg.Qbit.SavePath)
	}
	if clientCfg.Qbit.DownloadPath != "" {
		opts.DownloadPath = strings.TrimSpace(clientCfg.Qbit.DownloadPath)
	}
	if ok {
		opts.ContentLayout = contentLayout
	}

	if len(clientCfg.Qbit.Tags) > 0 {
		trimmed := make([]string, 0, len(clientCfg.Qbit.Tags))
		for _, tag := range clientCfg.Qbit.Tags {
			if t := strings.TrimSpace(tag); t != "" {
				trimmed = append(trimmed, t)
			}
		}
		if len(trimmed) > 0 {
			opts.Tags = strings.Join(trimmed, ",")
		}
	}

	return opts, domain.StatusSuccessfulHardlink, nil
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

func (p *processor) validateImportDestination(clientCfg *domain.Client) (domain.StatusCode, error) {
	expected := normalizePath(clientCfg.PreImportPath)

	if clientCfg.Qbit.SavePath != "" {
		actual := normalizePath(clientCfg.Qbit.SavePath)
		if actual != expected {
			return domain.StatusQbitConfigError, fmt.Errorf("%s: qbit.savePath(%s) must match preImportPath(%s)",
				domain.StatusQbitConfigError, clientCfg.Qbit.SavePath, clientCfg.PreImportPath)
		}
		return domain.StatusSuccessfulHardlink, nil
	}

	categories, err := p.req.Client.GetCategories()
	if err != nil {
		return domain.StatusQbitConfigError, fmt.Errorf("%s: could not read qbittorrent categories: %w",
			domain.StatusQbitConfigError, err)
	}

	category, ok := categories[clientCfg.Qbit.Category]
	if !ok {
		return domain.StatusQbitConfigError, fmt.Errorf("%s: qbit category %q was not found in qbittorrent",
			domain.StatusQbitConfigError, clientCfg.Qbit.Category)
	}

	actualPath := strings.TrimSpace(category.SavePath)
	if actualPath == "" || !filepath.IsAbs(filepath.FromSlash(actualPath)) {
		defaultSavePath, err := p.req.Client.GetDefaultSavePath()
		if err != nil {
			return domain.StatusQbitConfigError, fmt.Errorf("%s: could not read qbittorrent default save path: %w",
				domain.StatusQbitConfigError, err)
		}
		if actualPath == "" {
			actualPath = defaultSavePath
		} else {
			actualPath = filepath.Join(defaultSavePath, actualPath)
		}
	}

	actual := normalizePath(actualPath)
	if actual != expected {
		return domain.StatusQbitConfigError, fmt.Errorf("%s: qbittorrent destination(%s) must match preImportPath(%s)",
			domain.StatusQbitConfigError, actualPath, clientCfg.PreImportPath)
	}

	return domain.StatusSuccessfulHardlink, nil
}

func normalizePath(path string) string {
	return filepath.Clean(filepath.FromSlash(strings.TrimSpace(path)))
}

func (p *processor) waitForTorrent(hash string, timeout time.Duration) (qbittorrent.Torrent, domain.StatusCode, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		torrent, ok, err := p.lookupTorrent(hash)
		if err != nil {
			return qbittorrent.Torrent{}, domain.StatusFindTorrentError, fmt.Errorf("%s: %w", domain.StatusFindTorrentError, err)
		}
		if ok {
			return torrent, domain.StatusSuccessfulHardlink, nil
		}
		time.Sleep(250 * time.Millisecond)
	}

	return qbittorrent.Torrent{}, domain.StatusFindTorrentError,
		fmt.Errorf("%s: timed out waiting for hash %s", domain.StatusFindTorrentError, hash)
}

func (p *processor) waitForRecheck(hash string, timeout time.Duration) (qbittorrent.Torrent, domain.StatusCode, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		torrent, ok, err := p.lookupTorrent(hash)
		if err != nil {
			return qbittorrent.Torrent{}, domain.StatusFindTorrentError, fmt.Errorf("%s: %w", domain.StatusFindTorrentError, err)
		}
		if !ok {
			time.Sleep(250 * time.Millisecond)
			continue
		}

		switch torrent.State {
		case qbittorrent.TorrentStateCheckingDl, qbittorrent.TorrentStateCheckingUp, qbittorrent.TorrentStateCheckingResumeData,
			qbittorrent.TorrentStateMoving, qbittorrent.TorrentStateAllocating, qbittorrent.TorrentStateMissingFiles:
			time.Sleep(250 * time.Millisecond)
			continue
		default:
			return torrent, domain.StatusSuccessfulHardlink, nil
		}
	}

	return qbittorrent.Torrent{}, domain.StatusRecheckTorrentError,
		fmt.Errorf("%s: timed out waiting for recheck to complete for hash %s", domain.StatusRecheckTorrentError, hash)
}

func (p *processor) lookupTorrent(hash string) (qbittorrent.Torrent, bool, error) {
	torrents, err := p.req.Client.GetTorrents(qbittorrent.TorrentFilterOptions{Hashes: []string{hash}})
	if err != nil {
		return qbittorrent.Torrent{}, false, err
	}

	for _, torrent := range torrents {
		if strings.EqualFold(torrent.Hash, hash) || strings.EqualFold(torrent.InfohashV1, hash) {
			return torrent, true, nil
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

func buildHost(client *domain.Client) (string, error) {
	if len(client.Host) == 0 {
		return "", errors.New("host is required")
	}

	host := client.Host
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}

	parsedURL, err := url.Parse(host)
	if err != nil {
		return "", errors.Wrap(err, "failed to parse host")
	}

	if client.Port != 0 {
		parsedURL.Host = net.JoinHostPort(parsedURL.Hostname(), strconv.Itoa(client.Port))
	}

	return parsedURL.String(), nil
}
