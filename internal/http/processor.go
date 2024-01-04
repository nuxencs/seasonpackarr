package http

import (
	"encoding/json"
	"fmt"
	netHTTP "net/http"
	"path/filepath"
	"sync"
	"time"

	"seasonpackarr/internal/config"
	"seasonpackarr/internal/domain"
	"seasonpackarr/internal/logger"
	"seasonpackarr/internal/utils"

	"github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/rs/zerolog"
)

type processor struct {
	log zerolog.Logger
	cfg *config.AppConfig
	req *request
}

type entry struct {
	t qbittorrent.Torrent
	r rls.Release
}

type request struct {
	Name       string
	Torrent    json.RawMessage
	Client     *qbittorrent.Client
	ClientName string
}

type entryTime struct {
	e   map[string][]entry
	d   map[string]rls.Release
	t   time.Time
	err error
	sync.Mutex
}

type matchPaths struct {
	episodePath string
	packPath    string
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

func (p processor) getClient(client *domain.Client) error {
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

func (p processor) getAllTorrents(client *domain.Client) entryTime {
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

	torrents, err := p.req.Client.GetTorrents(qbittorrent.TorrentFilterOptions{})
	if err != nil {
		return entryTime{err: err}
	}

	nt := time.Now()
	res = &entryTime{e: make(map[string][]entry), t: nt.Add(nt.Sub(cur)), d: res.d}

	for _, t := range torrents {
		r, ok := res.d[t.Name]
		if !ok {
			r = rls.ParseString(t.Name)
			res.d[t.Name] = r
		}

		s := utils.GetFormattedTitle(r)
		res.e[s] = append(res.e[s], entry{t: t, r: r})
	}

	torrentMap.Store(set, res)
	return *res
}

func (p processor) getFiles(hash string) (*qbittorrent.TorrentFiles, error) {
	return p.req.Client.GetFilesInformation(hash)
}

func (p processor) ProcessSeasonPack(w netHTTP.ResponseWriter, r *netHTTP.Request) {
	if err := json.NewDecoder(r.Body).Decode(&p.req); err != nil {
		p.log.Error().Err(err).Msgf("error decoding request")
		netHTTP.Error(w, err.Error(), 470)
		return
	}

	clientName := p.req.ClientName

	client, ok := p.cfg.Config.Clients[clientName]
	if !ok {
		p.log.Info().Msgf("client not found in config: %q", clientName)

		// use default client
		clientName = "default"
		client = p.cfg.Config.Clients[clientName]

		p.log.Info().Msgf("using default client serving at %s:%d", client.Host, client.Port)
	}

	if len(p.req.Name) == 0 {
		p.log.Error().Msgf("error getting announce name")
		netHTTP.Error(w, fmt.Sprintf("error getting announce name"), 469)
		return
	}

	if err := p.getClient(client); err != nil {
		p.log.Error().Err(err).Msgf("error getting client")
		netHTTP.Error(w, fmt.Sprintf("error getting client: %q", err), 471)
		return
	}

	mp := p.getAllTorrents(client)
	if mp.err != nil {
		p.log.Error().Err(mp.err).Msgf("error getting torrents")
		netHTTP.Error(w, fmt.Sprintf("error getting torrents: %q", mp.err), 468)
		return
	}

	requestrls := entry{r: rls.ParseString(p.req.Name)}
	v, ok := mp.e[utils.GetFormattedTitle(requestrls.r)]
	if !ok {
		p.log.Info().Msgf("no matching releases in client %q: %q", clientName, p.req.Name)
		netHTTP.Error(w, fmt.Sprintf("no matching releases in client %q: %q", clientName, p.req.Name), 200)
		return
	}

	packDirName := utils.FormatSeasonPackTitle(p.req.Name)

	for _, child := range v {
		if checkCandidates(&requestrls, &child) == 210 {
			p.log.Info().Msgf("release already in client %q: %q", clientName, p.req.Name)
			netHTTP.Error(w, fmt.Sprintf("release already in client %q: %q", clientName, p.req.Name), 210)
			return
		}
	}

	for _, child := range v {
		switch res := checkCandidates(&requestrls, &child); res {
		case 210:
			p.log.Info().Msgf("release already in client %q: %q", clientName, p.req.Name)
			netHTTP.Error(w, fmt.Sprintf("release already in client %q: %q", clientName, p.req.Name), res)
			return

		case 211:
			p.log.Info().Msgf("release is not a season pack: %q", p.req.Name)
			netHTTP.Error(w, fmt.Sprintf("release is not a season pack: %q", p.req.Name), res)
			return

		case 250:
			m, err := p.getFiles(child.t.Hash)
			if err != nil {
				p.log.Error().Err(err).Msgf("error getting files: %q", child.t.Name)
				continue
			}

			fileName := ""
			for _, v := range *m {
				fileName = v.Name
				break
			}

			episodePath := filepath.Join(child.t.SavePath, fileName)
			packPath := filepath.Join(client.PreImportPath, packDirName, filepath.Base(fileName))

			currentMatch := []matchPaths{
				{
					episodePath: episodePath,
					packPath:    packPath,
				},
			}

			oldMatches, ok := matchesMap.Load(p.req.Name)
			if !ok {
				oldMatches = currentMatch
			}

			newMatches := append(oldMatches.([]matchPaths), currentMatch...)
			matchesMap.Store(p.req.Name, newMatches)
			continue
		}
	}

	if matchesSlice, ok := matchesMap.Load(p.req.Name); ok {
		if p.cfg.Config.ParseTorrentFile {
			p.log.Log().Msgf("found matching episodes for season pack: %q", p.req.Name)
			netHTTP.Error(w, fmt.Sprintf("found matching episodes for season pack: %q", p.req.Name), 250)
			return
		}

		matches := utils.DedupeSlice(matchesSlice.([]matchPaths))

		for _, match := range matches {
			if err := utils.CreateHardlink(match.episodePath, match.packPath); err != nil {
				p.log.Error().Err(err).Msgf("error creating hardlink for: %q", match.episodePath)
				netHTTP.Error(w, fmt.Sprintf("error creating hardlink for: %q", match.episodePath), 250)
				continue
			}
			p.log.Log().Msgf("created hardlink of %q into %q", match.episodePath, match.packPath)
			netHTTP.Error(w, fmt.Sprintf("created hardlink of %q into %q", match.episodePath, match.packPath), 250)
		}
	}
}

func (p processor) ParseTorrent(w netHTTP.ResponseWriter, r *netHTTP.Request) {
	if err := json.NewDecoder(r.Body).Decode(&p.req); err != nil {
		p.log.Error().Err(err).Msgf("error decoding request")
		netHTTP.Error(w, err.Error(), 470)
		return
	}

	if len(p.req.Name) == 0 {
		p.log.Error().Msgf("error getting announce name")
		netHTTP.Error(w, fmt.Sprintf("error getting announce name"), 469)
		return
	}

	if len(p.req.Torrent) == 0 {
		p.log.Error().Msgf("error getting torrent bytes")
		netHTTP.Error(w, fmt.Sprintf("error getting torrent bytes"), 468)
		return
	}

	torrentBytes, err := utils.DecodeTorrentDataRawBytes(p.req.Torrent)
	if err != nil {
		p.log.Error().Err(err).Msgf("error decoding torrent bytes")
		netHTTP.Error(w, fmt.Sprintf("error decoding torrent bytes: %q", err), 467)
		return
	}
	p.req.Torrent = torrentBytes

	folderName, err := utils.ParseFolderNameFromTorrentBytes(p.req.Torrent)
	if err != nil {
		p.log.Error().Err(err).Msgf("error parsing folder name")
		netHTTP.Error(w, fmt.Sprintf("error parsing folder name: %q", err), 466)
		return
	}

	matchesSlice, ok := matchesMap.Load(p.req.Name)
	if !ok {
		p.log.Info().Msgf("no matching releases in client: %q", p.req.Name)
		netHTTP.Error(w, fmt.Sprintf("no matching releases in client: %q", p.req.Name), 200)
		return
	}

	matches := utils.DedupeSlice(matchesSlice.([]matchPaths))

	for _, match := range matches {
		match.packPath = utils.ReplaceParentFolder(match.packPath, folderName)
		if err := utils.CreateHardlink(match.episodePath, match.packPath); err != nil {
			p.log.Error().Err(err).Msgf("error creating hardlink for: %q", match.episodePath)
			netHTTP.Error(w, fmt.Sprintf("error creating hardlink for: %q", match.episodePath), 250)
			continue
		}
		p.log.Log().Msgf("created hardlink of %q into %q", match.episodePath, match.packPath)
		netHTTP.Error(w, fmt.Sprintf("created hardlink of %q into %q", match.episodePath, match.packPath), 250)
	}
}

func checkCandidates(requestrls, child *entry) int {
	rlsRelease := requestrls.r
	rlsInClient := child.r

	// check if season pack and no extension
	if fmt.Sprint(rlsRelease.Type) == "series" && rlsRelease.Ext == "" {
		// compare formatted titles
		if utils.GetFormattedTitle(rlsInClient) == utils.GetFormattedTitle(rlsRelease) {
			// check if same episode
			if rlsInClient.Episode == rlsRelease.Episode {
				// release is already in client
				return 210
			}
			// season pack with matching episodes
			return 250
		}
	}
	// not a season pack
	return 211
}
