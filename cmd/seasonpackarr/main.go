package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"seasonpackarr/internal/config"
	"seasonpackarr/internal/logger"
	"seasonpackarr/internal/utils"

	"github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/moistari/rls"
	"github.com/spf13/pflag"
)

type Entry struct {
	t qbittorrent.Torrent
	r rls.Release
}

type request struct {
	Name string

	User     string
	Password string
	Host     string
	Port     uint

	Hash    string
	Torrent json.RawMessage
	Client  *qbittorrent.Client
}

type entryTime struct {
	e   map[string][]Entry
	d   map[string]rls.Release
	t   time.Time
	err error
	sync.Mutex
}

var (
	clientMap  sync.Map
	torrentMap sync.Map
)

var (
	log     logger.Logger
	cfg     *config.AppConfig
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	var configPath string

	pflag.StringVar(&configPath, "config", "", "path to configuration file")
	pflag.Parse()

	// read config
	cfg = config.New(configPath, version)

	// init new logger
	log = logger.New(cfg.Config)

	if err := cfg.UpdateConfig(); err != nil {
		log.Error().Err(err).Msgf("error updating config")
	}

	// init dynamic config
	cfg.DynamicReload(log)

	log.Info().Msgf("Starting seasonpackarr")
	log.Info().Msgf("Version: %s", version)
	log.Info().Msgf("Commit: %s", commit)
	log.Info().Msgf("Build date: %s", date)
	log.Info().Msgf("Log-level: %s", cfg.Config.LogLevel)

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.URLFormat)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Get("/api/health", heartbeat)

	r.Post("/api/pack", handleSeasonPack)
	err := http.ListenAndServe(fmt.Sprintf("%s:%d", cfg.Config.Host, cfg.Config.Port), r)
	if err != nil {
		log.Fatal().Err(err).Msgf("failed to listen on %s:%d", cfg.Config.Host, cfg.Config.Port)
	}
}

func getClient(req *request) error {
	s := qbittorrent.Config{
		Host:     req.Host,
		Username: req.User,
		Password: req.Password,
	}

	c, ok := clientMap.Load(s)
	if !ok {
		c = qbittorrent.NewClient(qbittorrent.Config{
			Host:     req.Host,
			Username: req.User,
			Password: req.Password,
		})

		if err := c.(*qbittorrent.Client).Login(); err != nil {
			log.Fatal().Err(err).Msg("failed to log into qBittorrent")
		}

		clientMap.Store(s, c)
	}

	req.Client = c.(*qbittorrent.Client)
	return nil
}

func heartbeat(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "alive", 200)
}

func (c *request) getAllTorrents() entryTime {
	set := qbittorrent.Config{
		Host:     c.Host,
		Username: c.User,
		Password: c.Password,
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

	torrents, err := c.Client.GetTorrents(qbittorrent.TorrentFilterOptions{})
	if err != nil {
		return entryTime{err: err}
	}

	nt := time.Now()
	res = &entryTime{e: make(map[string][]Entry), t: nt.Add(nt.Sub(cur)), d: res.d}

	for _, t := range torrents {
		r, ok := res.d[t.Name]
		if !ok {
			r = rls.ParseString(t.Name)
			res.d[t.Name] = r
		}

		s := utils.GetFormattedTitle(r)
		res.e[s] = append(res.e[s], Entry{t: t, r: r})
	}

	torrentMap.Store(set, res)
	return *res
}

func (c *request) getFiles(hash string) (*qbittorrent.TorrentFiles, error) {
	return c.Client.GetFilesInformation(hash)
}

func handleSeasonPack(w http.ResponseWriter, r *http.Request) {
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msgf("error decoding request")
		http.Error(w, err.Error(), 470)
		return
	}

	if len(req.Name) == 0 {
		log.Error().Msgf("no title passed")
		http.Error(w, fmt.Sprintf("no title passed\n"), 469)
		return
	}

	if err := getClient(&req); err != nil {
		log.Error().Err(err).Msgf("unable to get client")
		http.Error(w, fmt.Sprintf("unable to get client: %q\n", err), 471)
		return
	}

	mp := req.getAllTorrents()
	if mp.err != nil {
		log.Error().Err(mp.err).Msgf("unable to get torrents")
		http.Error(w, fmt.Sprintf("unable to get torrents: %q\n", mp.err), 468)
		return
	}

	requestrls := Entry{r: rls.ParseString(req.Name)}
	if v, ok := mp.e[utils.GetFormattedTitle(requestrls.r)]; ok {
		for _, child := range v {
			if checkCandidates(&requestrls, &child) == 210 {
				log.Info().Msgf("release already exists in client: %q", req.Name)
				http.Error(w, fmt.Sprintf("release already exists in client: %q\n", req.Name), 210)
				return
			}
		}
		for _, child := range v {
			res := checkCandidates(&requestrls, &child)

			if res == 210 {
				log.Info().Msgf("release already exists in client: %q", req.Name)
				http.Error(w, fmt.Sprintf("release already exists in client: %q\n", req.Name), res)
				break
			}
			if res == 211 {
				log.Info().Msgf("not a season pack: %q", req.Name)
				http.Error(w, fmt.Sprintf("not a season pack: %q\n", req.Name), res)
				break
			}
			if res == 250 {
				m, err := req.getFiles(child.t.Hash)
				if err != nil {
					log.Error().Err(err).Msgf("failed to get files for %q", req.Name)
					continue
				}

				fileName := ""
				for _, v := range *m {
					fileName = v.Name
					break
				}

				packDirName := utils.FormatSeasonPackTitle(req.Name)

				childPath := filepath.Join(child.t.SavePath, fileName)
				packPath := filepath.Join(cfg.Config.PreImportPath, packDirName, fileName)

				createHardlink(childPath, packPath)

				http.Error(w, fmt.Sprintf("created hardlink of %q into %q\n",
					childPath, packPath), res)
				continue
			}
		}
	} else {
		log.Info().Msgf("unique submission: %q", req.Name)
		http.Error(w, fmt.Sprintf("unique submission: %q\n", req.Name), 200)
	}
}

func checkCandidates(requestrls, child *Entry) int {
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

func createHardlink(srcPath string, trgPath string) {
	// create the target directory if it doesn't exist
	trgDir := filepath.Dir(trgPath)
	err := os.MkdirAll(trgDir, 0755)
	if err != nil {
		log.Error().Err(err).Msgf("error creating target directory: %s", trgDir)
	}

	if _, err = os.Stat(trgPath); os.IsNotExist(err) {
		// target file does not exist, create a hardlink
		err = os.Link(srcPath, trgPath)
		if err != nil {
			log.Error().Err(err).Msgf("error creating hardlink: %s", srcPath)
		}
		log.Info().Msgf("created hardlink of %q into %q", srcPath, trgPath)
	} else {
		log.Error().Msgf("file already exists, not creating hardlink: %s", srcPath)
	}
}
