// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build e2e

// Package e2e holds the end-to-end tests. They send one announce line into a
// real autobrr and check only what a user can see: the client API, the /data
// filesystem, and the autobrr release status or log. test/harness/run.sh e2e
// starts autobrr, the seasonpackarr image, the client, and the seeder.
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
	"net/http/cookiejar"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/daemontest"

	"github.com/stretchr/testify/assert"
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

// harness is the setup that all tests share: the torrent server, and autobrr
// when a test needs it. autobrr starts empty for each runner entry, so each
// setup runs once.
type harness struct {
	importDir string

	torrentURL string
	torrents   sync.Map // file name to .torrent bytes

	autobrr   *autobrrAPI
	networkID int
}

var (
	harnessOnce sync.Once
	shared      *harness
	harnessErr  error

	autobrrOnce sync.Once
	autobrrErr  error
)

// requireHarness returns the shared harness. Like daemontest.RequireSettings,
// it skips the test when a setting is missing, and fails it in strict mode.
func requireHarness(t *testing.T) *harness {
	t.Helper()
	daemontest.RequireSettings(t, envSeasonpackarrURL, envAPIToken, envTorrentURL, envImportDir)
	harnessOnce.Do(func() { shared, harnessErr = newHarness() })
	require.NoError(t, harnessErr, "set up the end-to-end harness")
	return shared
}

// requireAutobrr returns the shared harness with autobrr set up. Only the
// e2e-<client> entries start autobrr.
func requireAutobrr(t *testing.T) *harness {
	t.Helper()
	daemontest.RequireSettings(t, envAutobrrURL)
	h := requireHarness(t)
	autobrrOnce.Do(func() { autobrrErr = h.setupAutobrr() })
	require.NoError(t, autobrrErr, "set up autobrr")
	return h
}

func newHarness() (*harness, error) {
	h := &harness{torrentURL: os.Getenv(envTorrentURL), importDir: os.Getenv(envImportDir)}
	if err := h.serveTorrents(); err != nil {
		return nil, err
	}
	return h, nil
}

func (h *harness) setupAutobrr() error {
	api, err := setupAutobrr(os.Getenv(envAutobrrURL))
	if err != nil {
		return err
	}
	h.autobrr = api
	h.networkID, err = api.createIndexer(h.torrentURL)
	return err
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
// clientName. It uses the endpoints, payloads, header, query parameter, and
// expected status of docs/AUTOBRR.md.
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

// ensureFilter creates the filter of clientName once and links it to the indexer.
func (h *harness) ensureFilter(t *testing.T, clientName string) {
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
	client  *http.Client
	apiKey  string
}

// setupAutobrr creates the first user, logs in, and creates an API key. The
// cookie jar keeps the login session for the key request.
func setupAutobrr(baseURL string) (*autobrrAPI, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	api := &autobrrAPI{baseURL: strings.TrimSuffix(baseURL, "/"), client: &http.Client{Timeout: daemontest.Timeout, Jar: jar}}
	credentials := map[string]any{"username": "e2e", "password": "e2e-harness"}
	if err := api.do(http.MethodPost, "/api/auth/onboard", credentials, http.StatusOK, nil); err != nil {
		return nil, fmt.Errorf("onboard: %w", err)
	}
	if err := api.do(http.MethodPost, "/api/auth/login", credentials, http.StatusNoContent, nil); err != nil {
		return nil, fmt.Errorf("log in: %w", err)
	}

	var key struct {
		Key string `json:"key"`
	}
	if err := api.do(http.MethodPost, "/api/keys", map[string]any{"name": "e2e", "scopes": []string{}}, http.StatusCreated, &key); err != nil {
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

// findRelease returns the release named name, or a zero release when autobrr
// has none. autobrr stores a release only when a filter approves it.
func (a *autobrrAPI) findRelease(name string) (release, error) {
	var page struct {
		Data []release `json:"data"`
	}
	query := url.Values{"q": {name}, "indexer": {indexerIdentifier}, "limit": {"10"}}
	if err := a.do(http.MethodGet, "/api/release?"+query.Encode(), nil, http.StatusOK, &page); err != nil {
		return release{}, err
	}
	for _, r := range page.Data {
		if r.Name == name {
			return r, nil
		}
	}
	return release{}, nil
}

// waitReleaseDone waits until autobrr stored the release name and finished its
// actions, because the announce call returns when autobrr queued the line.
func (a *autobrrAPI) waitReleaseDone(t *testing.T, name string) release {
	t.Helper()
	var err error
	rls, done := daemontest.WaitFor(t.Context(), func() release {
		var r release
		r, err = a.findRelease(name)
		return r
	}, func(r release) bool {
		return err != nil || (len(r.ActionStatus) > 0 &&
			!slices.ContainsFunc(r.ActionStatus, func(s actionStatus) bool { return s.Status == "PENDING" }))
	})
	require.NoError(t, err, "read the autobrr release %s", name)
	require.True(t, done, "autobrr did not finish the release %s: %+v", name, rls)
	return rls
}

// requireImportSent waits until autobrr approved the release name and sent the
// import. The Webhook action ignores the import response, so a passed action
// proves only that autobrr sent the import.
func (h *harness) requireImportSent(t *testing.T, name string) {
	t.Helper()
	rls := h.autobrr.waitReleaseDone(t, name)
	assert.Equal(t, "FILTER_APPROVED", rls.FilterStatus, "autobrr filter status")
	for _, status := range rls.ActionStatus {
		assert.Equal(t, "PUSH_APPROVED", status.Status, "autobrr action %s: %v", status.Action, status.Rejections)
	}
}

// autobrrLogFile is the file name of AUTOBRR__LOG_PATH in compose.autobrr.yaml.
const autobrrLogFile = "autobrr.log"

// logLine is the part of an autobrr JSON log line that the tests check.
type logLine struct {
	Message        string `json:"message"`
	TraceID        string `json:"trace_id"`
	Filter         string `json:"filter"`
	Release        string `json:"release"`
	ExternalFilter string `json:"external_filter"`
	ActualStatus   int    `json:"actual_status"`
}

// rejection is a rejection of a release by an external webhook of a filter.
type rejection struct {
	// webhook is the name of the external webhook in testdata/filter.json.
	webhook string
	status  int
}

// findRejection returns the external webhook rejection that autobrr logged
// when filter rejected the release name. found is false until autobrr logged
// the rejection of the filter.
func (a *autobrrAPI) findRejection(filter, name string) (rej rejection, found bool, err error) {
	var log []byte
	if err := a.do(http.MethodGet, "/api/logs/files/"+autobrrLogFile, nil, http.StatusOK, &log); err != nil {
		return rejection{}, false, err
	}
	var lines []logLine
	for raw := range bytes.Lines(log) {
		var line logLine
		if json.Unmarshal(raw, &line) == nil {
			lines = append(lines, line)
		}
	}
	index := slices.IndexFunc(lines, func(l logLine) bool {
		return l.Message == "filter rejected release" && l.Filter == filter && l.Release == name
	})
	if index < 0 {
		return rejection{}, false, nil
	}
	// The webhook line has no release name. The trace ID of the announce links it.
	for _, line := range lines[:index] {
		if line.Message == "external webhook got unexpected status code" && line.TraceID == lines[index].TraceID {
			rej = rejection{webhook: line.ExternalFilter, status: line.ActualStatus}
		}
	}
	return rej, true, nil
}

// waitRejection waits until autobrr logged that filter rejected the release
// name, and returns the webhook rejection. autobrr stores no release row for a
// rejected announce, so its log is the only record of the rejection.
func (a *autobrrAPI) waitRejection(t *testing.T, filter, name string) rejection {
	t.Helper()
	var (
		rej rejection
		err error
	)
	_, logged := daemontest.WaitFor(t.Context(), func() bool {
		var found bool
		rej, found, err = a.findRejection(filter, name)
		return found
	}, func(found bool) bool { return err != nil || found })
	require.NoError(t, err, "read the autobrr log")
	require.True(t, logged, "autobrr did not log a rejection of %s by the filter %s", name, filter)
	return rej
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

// do sends body as JSON and decodes the response into out when out is not
// nil. A *[]byte out gets the response body as it is.
func (a *autobrrAPI) do(method, path string, body any, wantStatus int, out any) error {
	req, err := a.request(method, path, body)
	if err != nil {
		return err
	}
	return a.send(req, wantStatus, out)
}

func (a *autobrrAPI) send(req *http.Request, wantStatus int, out any) error {
	res, err := a.client.Do(req)
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
	switch out := out.(type) {
	case nil:
		return nil
	case *[]byte:
		*out = data
		return nil
	default:
		return json.Unmarshal(data, out)
	}
}
