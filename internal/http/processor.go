package http

import (
	"encoding/json"
	"fmt"
	"golang.org/x/exp/slices"
	netHTTP "net/http"
	"path/filepath"
	"seasonpackarr/internal/domain"
	"sync"
	"time"

	"seasonpackarr/internal/config"
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

var (
	clientMap  sync.Map
	torrentMap sync.Map
)

func newProcessor(log logger.Logger, config *config.AppConfig) *processor {
	return &processor{
		log: log.With().Str("module", "processor").Logger(),
		cfg: config,
	}
}

func (p processor) getClient(clientIndex int) error {
	s := qbittorrent.Config{
		Host:     fmt.Sprintf("http://%s:%d", p.cfg.Config.Clients[clientIndex].Host, p.cfg.Config.Clients[clientIndex].Port),
		Username: p.cfg.Config.Clients[clientIndex].Username,
		Password: p.cfg.Config.Clients[clientIndex].Password,
	}

	c, ok := clientMap.Load(s)
	if !ok {
		c = qbittorrent.NewClient(qbittorrent.Config{
			Host:     fmt.Sprintf("http://%s:%d", p.cfg.Config.Clients[clientIndex].Host, p.cfg.Config.Clients[clientIndex].Port),
			Username: p.cfg.Config.Clients[clientIndex].Username,
			Password: p.cfg.Config.Clients[clientIndex].Password,
		})

		if err := c.(*qbittorrent.Client).Login(); err != nil {
			p.log.Fatal().Err(err).Msg("error logging into qBittorrent")
		}

		clientMap.Store(s, c)
	}

	p.req.Client = c.(*qbittorrent.Client)
	return nil
}

func (p processor) getAllTorrents(clientIndex int) entryTime {
	set := qbittorrent.Config{
		Host:     fmt.Sprintf("http://%s:%d", p.cfg.Config.Clients[clientIndex].Host, p.cfg.Config.Clients[clientIndex].Port),
		Username: p.cfg.Config.Clients[clientIndex].Username,
		Password: p.cfg.Config.Clients[clientIndex].Password,
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

	clientIndex := findClientIndex(p.cfg.Config, p.req.ClientName)

	if clientIndex == -1 {
		// default to first client in config
		p.log.Info().Msgf("client not found in config: %q", p.req.ClientName)
		p.log.Info().Msgf("using first client defined in config: %q ", p.cfg.Config.Clients[0].Name)

		clientIndex = 0
	}

	if len(p.req.Name) == 0 {
		p.log.Error().Msgf("error getting announce name")
		netHTTP.Error(w, fmt.Sprintf("error getting announce name"), 469)
		return
	}

	if err := p.getClient(clientIndex); err != nil {
		p.log.Error().Err(err).Msgf("error getting client")
		netHTTP.Error(w, fmt.Sprintf("error getting client: %q", err), 471)
		return
	}

	mp := p.getAllTorrents(clientIndex)
	if mp.err != nil {
		p.log.Error().Err(mp.err).Msgf("error getting torrents")
		netHTTP.Error(w, fmt.Sprintf("error getting torrents: %q", mp.err), 468)
		return
	}

	requestrls := entry{r: rls.ParseString(p.req.Name)}
	v, ok := mp.e[utils.GetFormattedTitle(requestrls.r)]
	if !ok {
		p.log.Info().Msgf("no matching releases in client %q: %q", p.cfg.Config.Clients[clientIndex].Name, p.req.Name)
		netHTTP.Error(w, fmt.Sprintf("no matching releases in client %q: %q", p.cfg.Config.Clients[clientIndex].Name, p.req.Name), 200)
	}

	packDirName := utils.FormatSeasonPackTitle(p.req.Name)

	for _, child := range v {
		if checkCandidates(&requestrls, &child) == 210 {
			p.log.Info().Msgf("release already in client %q: %q", p.cfg.Config.Clients[clientIndex].Name, p.req.Name)
			netHTTP.Error(w, fmt.Sprintf("release already in client %q: %q", p.cfg.Config.Clients[clientIndex].Name, p.req.Name), 210)
			return
		}
	}

	for _, child := range v {
		switch res := checkCandidates(&requestrls, &child); res {
		case 210:
			p.log.Info().Msgf("release already in client %q: %q", p.cfg.Config.Clients[clientIndex].Name, p.req.Name)
			netHTTP.Error(w, fmt.Sprintf("release already in client %q: %q", p.cfg.Config.Clients[clientIndex].Name, p.req.Name), res)
			break

		case 211:
			p.log.Info().Msgf("release is not a season pack: %q", p.req.Name)
			netHTTP.Error(w, fmt.Sprintf("release is not a season pack: %q", p.req.Name), res)
			break

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

			childPath := filepath.Join(child.t.SavePath, fileName)
			packPath := filepath.Join(p.cfg.Config.Clients[clientIndex].PreImportPath, packDirName, fileName)

			err = utils.CreateHardlink(childPath, packPath)
			if err != nil {
				p.log.Error().Err(err).Msgf("error creating hardlink for: %q", childPath)
				netHTTP.Error(w, fmt.Sprintf("error creating hardlink for: %q", childPath), res)
				continue
			}

			p.log.Log().Msgf("created hardlink of %q into %q", childPath, packPath)
			netHTTP.Error(w, fmt.Sprintf("created hardlink of %q into %q", childPath, packPath), res)
			continue
		}
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

func findClientIndex(config *domain.Config, clientName string) int {
	idx := slices.IndexFunc(config.Clients, func(c *domain.Client) bool {
		return c.Name == clientName
	})
	return idx
}
