// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build e2e

// Package e2e holds the end-to-end tests. They send one announce line into a
// real autobrr and check only what a user can see: the client API, the /data
// filesystem, and the autobrr release status. test/harness/run.sh e2e starts
// autobrr, the seasonpackarr image, the client, and the seeder.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/daemontest"

	"github.com/stretchr/testify/require"
)

const (
	envAutobrrURL       = "SEASONPACKARR_TEST_E2E_AUTOBRR_URL"
	envSeasonpackarrURL = "SEASONPACKARR_TEST_E2E_SEASONPACKARR_URL"
	envAPIToken         = "SEASONPACKARR_TEST_E2E_API_TOKEN"
	// envTorrentURL is the address at which autobrr reaches the torrent server
	// of the driver. The driver listens on its port.
	envTorrentURL = "SEASONPACKARR_TEST_E2E_TORRENT_URL"
	envImportDir  = "SEASONPACKARR_TEST_IMPORT_DIR"
)

// The values in test/harness/autobrr/definitions/seasonpackarr-e2e.yaml.
const (
	indexerIdentifier = "seasonpackarr-e2e"
	ircServer         = "irc.seasonpackarr-e2e.invalid"
	ircPort           = 6667
	// announceChannel is the definition channel in lowercase and without '#',
	// as the announce-process path needs it.
	announceChannel = "announces"
)

// harness is the autobrr setup and the torrent server that all tests share.
// autobrr starts empty for each runner entry, so the setup runs once.
type harness struct {
	autobrr   *autobrrAPI
	networkID int
	importDir string

	torrentURL string
	torrents   sync.Map // file name to .torrent bytes
}

var (
	harnessOnce sync.Once
	shared      *harness
	harnessErr  error
)

// requireHarness returns the shared harness. Like daemontest.RequireSettings,
// it skips the test when a setting is missing, and fails it in strict mode.
func requireHarness(t *testing.T) *harness {
	t.Helper()
	daemontest.RequireSettings(t, envAutobrrURL, envSeasonpackarrURL, envAPIToken, envTorrentURL, envImportDir)
	harnessOnce.Do(func() { shared, harnessErr = newHarness() })
	require.NoError(t, harnessErr, "set up the end-to-end harness")
	return shared
}

func newHarness() (*harness, error) {
	h := &harness{torrentURL: os.Getenv(envTorrentURL), importDir: os.Getenv(envImportDir)}
	if err := h.serveTorrents(); err != nil {
		return nil, err
	}
	api, err := setupAutobrr(os.Getenv(envAutobrrURL))
	if err != nil {
		return nil, err
	}
	h.autobrr = api
	if h.networkID, err = api.createIndexer(h.torrentURL); err != nil {
		return nil, err
	}
	return h, nil
}

// serveTorrents serves the .torrent files that tests add with addTorrent, for
// the rest of the test process. autobrr downloads them with a plain GET.
func (h *harness) serveTorrents() error {
	u, err := url.Parse(h.torrentURL)
	if err != nil {
		return fmt.Errorf("parse %s: %w", envTorrentURL, err)
	}
	listener, err := net.Listen("tcp", ":"+u.Port())
	if err != nil {
		return fmt.Errorf("listen for torrent downloads: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /torrents/{file}", func(w http.ResponseWriter, r *http.Request) {
		torrent, ok := h.torrents.Load(r.PathValue("file"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/x-bittorrent")
		_, _ = w.Write(torrent.([]byte))
	})
	go func() { _ = http.Serve(listener, mux) }()
	return nil
}

// announce serves torrent and sends one announce line for release name into
// autobrr. autobrr only queues the line, so the caller must wait for the result.
func (h *harness) announce(t *testing.T, name string, torrent []byte) {
	t.Helper()
	file := name + ".torrent"
	h.torrents.Store(file, torrent)
	line := fmt.Sprintf("New Torrent: %s | %s/torrents/%s", name, h.torrentURL, file)
	t.Logf("announce: %s", line)
	path := fmt.Sprintf("/api/irc/network/%d/channel/%s/announce/process", h.networkID, announceChannel)
	require.NoError(t, h.autobrr.do(http.MethodPost, path, map[string]string{"msg": line}, http.StatusNoContent, nil),
		"send the announce to autobrr")
}

// filterConfig reads testdata/filter.json and fills in its placeholders for
// clientName. It uses the README endpoints, payloads, header, query parameter,
// and expected status.
func filterConfig(t *testing.T, clientName string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("testdata/filter.json")
	require.NoError(t, err)
	filled := strings.NewReplacer(
		"${SEASONPACKARR_URL}", os.Getenv(envSeasonpackarrURL),
		"${API_TOKEN}", os.Getenv(envAPIToken),
		"${CLIENT_NAME}", clientName,
	).Replace(string(raw))
	var filter map[string]any
	require.NoError(t, json.Unmarshal([]byte(filled), &filter), "decode the filled-in filter config")
	return filter
}

// requireFilter creates the filter of clientName once and links it to the indexer.
func (h *harness) requireFilter(t *testing.T, clientName string) {
	t.Helper()
	filter := filterConfig(t, clientName)
	if h.autobrr.hasFilter(t, filter["name"].(string)) {
		return
	}
	require.NoError(t, h.autobrr.createFilter(filter), "create the autobrr filter")
}

// packName is a unique pack release name, so reruns and other tests do not
// share releases. The title has only letters, so the release parsers of
// autobrr and seasonpackarr see the plain title.
func packName() string {
	title := make([]byte, 8)
	for i := range title {
		title[i] = byte('a' + rand.IntN(26))
	}
	title[0] -= 'a' - 'A'
	return "Seasonpackarr.Harness." + string(title) + ".S01.1080p.WEB-DL.H.264-RlsGrp"
}

// autobrrAPI calls the autobrr REST API with an API key after setup.
type autobrrAPI struct {
	baseURL string
	http    *http.Client
	apiKey  string
}

// setupAutobrr creates the first user, logs in, and creates an API key.
func setupAutobrr(baseURL string) (*autobrrAPI, error) {
	const user, password = "e2e", "e2e-harness"
	api := &autobrrAPI{baseURL: strings.TrimSuffix(baseURL, "/"), http: &http.Client{Timeout: daemontest.Timeout}}
	credentials := map[string]any{"username": user, "password": password}
	if err := api.do(http.MethodPost, "/api/auth/onboard", credentials, http.StatusOK, nil); err != nil {
		return nil, fmt.Errorf("onboard: %w", err)
	}

	body, err := json.Marshal(credentials)
	if err != nil {
		return nil, err
	}
	res, err := api.http.Post(api.baseURL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("log in: %w", err)
	}
	_ = res.Body.Close()
	cookies := res.Cookies()
	if res.StatusCode != http.StatusNoContent || len(cookies) == 0 {
		return nil, fmt.Errorf("log in: status %d, %d cookies", res.StatusCode, len(cookies))
	}

	var key struct {
		Key string `json:"key"`
	}
	req, err := api.request(http.MethodPost, "/api/keys", map[string]any{"name": "e2e", "scopes": []string{}})
	if err != nil {
		return nil, err
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	if err := api.send(req, http.StatusCreated, &key); err != nil {
		return nil, fmt.Errorf("create API key: %w", err)
	}
	if key.Key == "" {
		return nil, fmt.Errorf("create API key: empty key")
	}
	api.apiKey = key.Key
	return api, nil
}

// createIndexer creates the indexer from the custom definition, then its
// enabled IRC network. autobrr connects the announce channel to the indexer
// only when the indexer exists first. baseURL replaces the host of every
// announced torrent URL.
func (a *autobrrAPI) createIndexer(baseURL string) (int, error) {
	// autobrr answers health checks before it loads the custom definitions.
	var schema []named
	_, loaded := daemontest.WaitFor(context.Background(), func() error {
		return a.do(http.MethodGet, "/api/indexer/schema", nil, http.StatusOK, &schema)
	}, func(err error) bool {
		return err == nil && slices.ContainsFunc(schema, func(d named) bool { return d.Identifier == indexerIdentifier })
	})
	if !loaded {
		return 0, fmt.Errorf("autobrr did not load the definition %s", indexerIdentifier)
	}

	indexer := map[string]any{
		"name":                "Seasonpackarr E2E",
		"identifier":          indexerIdentifier,
		"identifier_external": "Seasonpackarr E2E",
		"implementation":      "irc",
		"enabled":             true,
		"base_url":            strings.TrimSuffix(baseURL, "/") + "/",
		"settings":            map[string]string{},
	}
	if err := a.do(http.MethodPost, "/api/indexer", indexer, http.StatusCreated, nil); err != nil {
		return 0, fmt.Errorf("create indexer: %w", err)
	}

	// The server does not exist. autobrr retries the connection with a backoff
	// and keeps the network, its channels, and the announce queue.
	network := map[string]any{
		"name":     "Seasonpackarr E2E",
		"enabled":  true,
		"server":   ircServer,
		"port":     ircPort,
		"nick":     "seasonpackarr-e2e",
		"auth":     map[string]string{"mechanism": "NONE"},
		"channels": []map[string]any{{"name": "#" + announceChannel, "enabled": true}},
	}
	if err := a.do(http.MethodPost, "/api/irc", network, http.StatusNoContent, nil); err != nil {
		return 0, fmt.Errorf("create IRC network: %w", err)
	}
	var networks []struct {
		ID     int    `json:"id"`
		Server string `json:"server"`
	}
	if err := a.do(http.MethodGet, "/api/irc", nil, http.StatusOK, &networks); err != nil {
		return 0, fmt.Errorf("list IRC networks: %w", err)
	}
	for _, n := range networks {
		if n.Server == ircServer {
			return n.ID, nil
		}
	}
	return 0, fmt.Errorf("IRC network %s not found", ircServer)
}

func (a *autobrrAPI) hasFilter(t *testing.T, name string) bool {
	t.Helper()
	var filters []named
	require.NoError(t, a.do(http.MethodGet, "/api/filters", nil, http.StatusOK, &filters), "list autobrr filters")
	return slices.ContainsFunc(filters, func(f named) bool { return f.Name == name })
}

// createFilter creates the base filter like autobrr's filter import, then
// updates the stored filter with its indexer, external webhooks, and actions.
// autobrr stores those only on the update.
func (a *autobrrAPI) createFilter(filter map[string]any) error {
	base := map[string]any{}
	for key, value := range filter {
		if key != "external" && key != "actions" {
			base[key] = value
		}
	}
	var created named
	if err := a.do(http.MethodPost, "/api/filters", withRequiredArrays(base), http.StatusCreated, &created); err != nil {
		return fmt.Errorf("create: %w", err)
	}

	var indexers []named
	if err := a.do(http.MethodGet, "/api/indexer", nil, http.StatusOK, &indexers); err != nil {
		return fmt.Errorf("list indexers: %w", err)
	}
	index := slices.IndexFunc(indexers, func(i named) bool { return i.Identifier == indexerIdentifier })
	if index < 0 {
		return fmt.Errorf("indexer %s not found", indexerIdentifier)
	}

	// The update overwrites every field, so it sends the stored filter back.
	path := fmt.Sprintf("/api/filters/%d", created.ID)
	var stored map[string]any
	if err := a.do(http.MethodGet, path, nil, http.StatusOK, &stored); err != nil {
		return fmt.Errorf("read: %w", err)
	}
	stored["indexers"] = []map[string]int{{"id": indexers[index].ID}}
	stored["external"] = filter["external"]
	stored["actions"] = filter["actions"]
	if err := a.do(http.MethodPut, path, withRequiredArrays(stored), http.StatusOK, nil); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	return nil
}

// withRequiredArrays sets the filter arrays that autobrr stores as NOT NULL to
// empty when filter omits them. autobrr's filter import does the same, and its
// filter reads omit empty arrays.
func withRequiredArrays(filter map[string]any) map[string]any {
	for _, key := range []string{"resolutions", "sources", "codecs", "containers"} {
		if filter[key] == nil {
			filter[key] = []string{}
		}
	}
	return filter
}

// release is the part of an autobrr release that the tests check.
type release struct {
	Name         string         `json:"name"`
	FilterStatus string         `json:"filter_status"`
	ActionStatus []actionStatus `json:"action_status"`
}

type actionStatus struct {
	Action     string   `json:"action"`
	Status     string   `json:"status"`
	Rejections []string `json:"rejections"`
}

// named is the identity of an autobrr indexer, definition, or filter.
type named struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Identifier string `json:"identifier"`
}

// findRelease returns the release named name. autobrr stores a release only
// when a filter approves it.
func (a *autobrrAPI) findRelease(name string) (release, bool, error) {
	var page struct {
		Data []release `json:"data"`
	}
	query := url.Values{"q": {name}, "indexer": {indexerIdentifier}, "limit": {"10"}}
	if err := a.do(http.MethodGet, "/api/release?"+query.Encode(), nil, http.StatusOK, &page); err != nil {
		return release{}, false, err
	}
	for _, r := range page.Data {
		if r.Name == name {
			return r, true, nil
		}
	}
	return release{}, false, nil
}

// waitReleaseDone waits until autobrr stored the release name and finished its
// actions, because the announce call returns when autobrr queued the line.
func (a *autobrrAPI) waitReleaseDone(t *testing.T, name string) release {
	t.Helper()
	var err error
	rls, done := daemontest.WaitFor(t.Context(), func() release {
		var r release
		r, _, err = a.findRelease(name)
		return r
	}, func(r release) bool {
		return err != nil || (len(r.ActionStatus) > 0 &&
			!slices.ContainsFunc(r.ActionStatus, func(s actionStatus) bool { return s.Status == "PENDING" }))
	})
	require.NoError(t, err, "read the autobrr release %s", name)
	require.True(t, done, "autobrr did not finish the release %s: %+v", name, rls)
	return rls
}

func (a *autobrrAPI) request(method, path string, body any) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, a.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.apiKey != "" {
		req.Header.Set("X-API-Token", a.apiKey)
	}
	return req, nil
}

// do sends body as JSON and decodes the response into out when out is not nil.
func (a *autobrrAPI) do(method, path string, body any, wantStatus int, out any) error {
	req, err := a.request(method, path, body)
	if err != nil {
		return err
	}
	return a.send(req, wantStatus, out)
}

func (a *autobrrAPI) send(req *http.Request, wantStatus int, out any) error {
	res, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode != wantStatus {
		return fmt.Errorf("%s %s: status %d, want %d: %s", req.Method, req.URL.Path, res.StatusCode, wantStatus, data)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}
