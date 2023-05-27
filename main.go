package main

import (
	"encoding/json"
	"fmt"
	"github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/moistari/rls"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
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
	torrentFilesPath = "/data/torrents"
	preImportDir     = "tv-hd"
	clientMap        sync.Map
	torrentMap       sync.Map
)

func main() {
	zerolog.TimeFieldFormat = time.RFC3339
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	log.Logger = zerolog.New(os.Stdout).With().Timestamp().Stack().Logger()

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.URLFormat)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Get("/api/health", heartbeat)

	r.Post("/api/pack", handleSeasonPack)
	err := http.ListenAndServe(":42069", r)
	if err != nil {
		log.Fatal().Msgf("Error listening on port 42069: %s", err)
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
			log.Fatal().Msgf("Error logging into qBittorrent: %s", err)
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

		s := getFormattedTitle(r)
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
		http.Error(w, err.Error(), 470)
		return
	}

	if len(req.Name) == 0 {
		http.Error(w, fmt.Sprintf("No title passed.\n"), 469)
		return
	}

	if err := getClient(&req); err != nil {
		http.Error(w, fmt.Sprintf("Unable to get client: %q\n", err), 471)
		return
	}

	mp := req.getAllTorrents()
	if mp.err != nil {
		http.Error(w, fmt.Sprintf("Unable to get result: %q\n", mp.err), 468)
		return
	}

	requestrls := Entry{r: rls.ParseString(req.Name)}
	if v, ok := mp.e[getFormattedTitle(requestrls.r)]; ok {
		existsInClient := false
		for _, child := range v {
			if rls.Compare(requestrls.r, child.r) == 0 {
				existsInClient = true
				http.Error(w, fmt.Sprintf("release already exists in client: %q\n", req.Name), 210)
				break
			}
		}
		if !existsInClient {
			for _, child := range v {
				res := checkCandidates(&requestrls, &child)

				if res == 210 {
					http.Error(w, fmt.Sprintf("release already exists in client: %q\n", req.Name), 210)
					break
				}
				if res == 211 {
					http.Error(w, fmt.Sprintf("not a season pack: %q\n", req.Name), 211)
					break
				}
				if res == 250 {
					preImportPath := filepath.Join(torrentFilesPath, preImportDir)

					m, err := req.getFiles(child.t.Hash)
					if err != nil {
						fmt.Printf("Failed to get Files %q: %q\n", req.Name, err)
						continue
					}

					fileName := ""
					for _, v := range *m {
						fileName = v.Name
						break
					}

					packDirName := formatSeasonPackTitle(req.Name)

					childPath := filepath.Join(child.t.SavePath, fileName)
					packPath := filepath.Join(preImportPath, packDirName, fileName)

					createHardlink(childPath, packPath)

					http.Error(w, fmt.Sprintf("created hardlink of %q into folder %q\n",
						childPath, packPath), 250)
					continue
				}
			}
		}
	} else {
		http.Error(w, fmt.Sprintf("unique submission: %q\n", req.Name), 200)
	}
}

func checkCandidates(requestrls, child *Entry) int {
	rlsRelease := requestrls.r
	rlsInClient := child.r

	// check if season pack and no extension
	if fmt.Sprint(rlsRelease.Type) == "series" && rlsRelease.Ext == "" {
		// compare formatted titles
		if getFormattedTitle(rlsInClient) == getFormattedTitle(rlsRelease) {
			if rlsInClient.Episode != rlsRelease.Episode {
				// check if same episode and if season pack
				log.Info().Msgf("create hardlink of %q into season pack folder", rlsInClient)
				return 250
			}
			log.Info().Msgf("release already exists in client")
			return 210
		}
	}
	// not season pack
	log.Info().Msgf("not a season pack")
	return 211
}

func getFormattedTitle(r rls.Release) string {
	s := fmt.Sprintf("%s%d%d%s%s%s%s", rls.MustNormalize(r.Title), r.Year, r.Series,
		rls.MustNormalize(r.Resolution), rls.MustNormalize(r.Source),
		fmt.Sprintf("%s", r.HDR), r.Group)
	for _, a := range r.Cut {
		s += rls.MustNormalize(a)
	}

	for _, a := range r.Edition {
		s += rls.MustNormalize(a)
	}

	for _, a := range r.Other {
		s += rls.MustNormalize(a)
	}

	re := regexp.MustCompile(`(?i)(?:\d{3,4}p|Repack\d?|Proper\d?|Real)[-_. ](\w+)[-_. ]WEB`)
	service := re.FindStringSubmatch(fmt.Sprintf("%q", r))
	if len(service) > 1 {
		s += rls.MustNormalize(service[1])
	}

	return s
}

func createHardlink(srcPath string, trgPath string) {
	// create the target directory if it doesn't exist
	trgDir := filepath.Dir(trgPath)
	err := os.MkdirAll(trgDir, 0755)
	if err != nil {
		log.Error().Msgf("could not create target directory %s: %v", trgDir, err)
	}

	if _, err := os.Stat(trgPath); os.IsNotExist(err) {
		// target file does not exist, create a hardlink
		err = os.Link(srcPath, trgPath)
		if err != nil {
			log.Error().Msgf("could not create hardlink for %s: %v", srcPath, err)
		}
		log.Info().Msgf("successfully created hardlink for %s", srcPath)
	} else {
		log.Error().Msgf("target file already exists, not creating hardlink for %s", srcPath)
	}
}

func formatSeasonPackTitle(packName string) string {
	// replace spaces with periods
	packName = strings.ReplaceAll(packName, " ", ".")
	// replace wrong audio naming
	packName = strings.ReplaceAll(packName, "DDP.5.1", "DDP5.1")

	return packName
}
