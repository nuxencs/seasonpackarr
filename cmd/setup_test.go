// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"encoding/json/v2"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fakeAutobrrKey = "autobrr-key"

// capturedRequest is one request that fakeAutobrrAPI received.
type capturedRequest struct {
	method, path string
	body         map[string]any
}

// fakeAutobrrAPI stores filters like the autobrr REST API v1.88.0: a create
// stores only the base settings, a read omits the empty arrays, and an update
// keeps the stored actions that the body leaves out.
type fakeAutobrrAPI struct {
	mu           sync.Mutex
	filters      map[int]map[string]any
	nextID       int
	nextActionID int
	requests     []capturedRequest
	// failPath answers requests to this method and path with HTTP 500.
	failPath string
	// hideActionsOnce omits the actions from the next filter read, like autobrr
	// after a failed action lookup.
	hideActionsOnce bool
}

func newFakeAutobrrAPI(t *testing.T) (*fakeAutobrrAPI, *httptest.Server) {
	t.Helper()
	f := &fakeAutobrrAPI{filters: map[int]map[string]any{}, nextID: 1, nextActionID: 1}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/filters", func(w http.ResponseWriter, r *http.Request) {
		body := maps.Clone(f.record(t, r))
		delete(body, "external")
		delete(body, "actions")
		f.mu.Lock()
		body["id"] = f.nextID
		f.filters[f.nextID] = body
		f.nextID++
		f.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		assert.NoError(t, json.MarshalWrite(w, body))
	})
	mux.HandleFunc("GET /api/filters/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.record(t, r)
		stored, ok := f.filter(r.PathValue("id"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		read := map[string]any{}
		f.mu.Lock()
		hide := f.hideActionsOnce
		f.hideActionsOnce = false
		f.mu.Unlock()
		for key, value := range stored {
			if hide && key == "actions" {
				continue
			}
			if values, isArray := value.([]any); isArray && len(values) == 0 {
				continue
			}
			read[key] = value
		}
		assert.NoError(t, json.MarshalWrite(w, read))
	})
	mux.HandleFunc("GET /api/filters", func(w http.ResponseWriter, r *http.Request) {
		f.record(t, r)
		f.mu.Lock()
		list := []map[string]any{}
		for _, id := range slices.Sorted(maps.Keys(f.filters)) {
			list = append(list, map[string]any{"id": id, "name": f.filters[id]["name"], "enabled": f.filters[id]["enabled"]})
		}
		f.mu.Unlock()
		assert.NoError(t, json.MarshalWrite(w, list))
	})
	mux.HandleFunc("PUT /api/filters/{id}", func(w http.ResponseWriter, r *http.Request) {
		body := f.record(t, r)
		id, err := strconv.Atoi(r.PathValue("id"))
		assert.NoError(t, err)
		f.mu.Lock()
		defer f.mu.Unlock()
		stored, ok := f.filters[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		// autobrr updates the actions with an ID, adds the others, and keeps the stored actions.
		actions, _ := stored["actions"].([]any)
		sent, _ := body["actions"].([]any)
		for _, value := range sent {
			action := value.(map[string]any)
			if actionID, _ := action["id"].(float64); actionID > 0 {
				index := slices.IndexFunc(actions, func(a any) bool { return a.(map[string]any)["id"] == action["id"] })
				if assert.GreaterOrEqual(t, index, 0, "unknown action %v", action["id"]) {
					actions[index] = action
				}
				continue
			}
			action["id"] = float64(f.nextActionID)
			f.nextActionID++
			actions = append(actions, action)
		}
		body["actions"] = actions
		f.filters[id] = body
		assert.NoError(t, json.MarshalWrite(w, body))
	})
	mux.HandleFunc("DELETE /api/actions/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.record(t, r)
		actionID, err := strconv.Atoi(r.PathValue("id"))
		assert.NoError(t, err)
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, stored := range f.filters {
			actions, _ := stored["actions"].([]any)
			index := slices.IndexFunc(actions, func(a any) bool { return a.(map[string]any)["id"] == float64(actionID) })
			if index >= 0 {
				stored["actions"] = slices.Delete(actions, index, index+1)
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		http.NotFound(w, r)
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Token") != fakeAutobrrKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.mu.Lock()
		fail := f.failPath == r.Method+" "+r.URL.Path
		f.mu.Unlock()
		if fail {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return f, server
}

func (f *fakeAutobrrAPI) record(t *testing.T, r *http.Request) map[string]any {
	var body map[string]any
	if r.Method != http.MethodGet && r.Method != http.MethodDelete {
		assert.NoError(t, json.UnmarshalRead(r.Body, &body))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, capturedRequest{method: r.Method, path: r.URL.Path, body: body})
	return body
}

func (f *fakeAutobrrAPI) filter(id string) (map[string]any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, err := strconv.Atoi(id)
	if err != nil {
		return nil, false
	}
	stored, ok := f.filters[n]
	return stored, ok
}

// seed stores filter as a user made it in autobrr, with an ID for each action.
func (f *fakeAutobrrAPI) seed(t *testing.T, filter string) int {
	t.Helper()
	var stored map[string]any
	require.NoError(t, json.Unmarshal([]byte(filter), &stored))
	f.mu.Lock()
	defer f.mu.Unlock()
	actions, _ := stored["actions"].([]any)
	for _, action := range actions {
		action.(map[string]any)["id"] = float64(f.nextActionID)
		f.nextActionID++
	}
	id := f.nextID
	f.nextID++
	stored["id"] = float64(id)
	f.filters[id] = stored
	return id
}

// writes returns the requests that change autobrr state.
func (f *fakeAutobrrAPI) writes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var writes []string
	for _, request := range f.requests {
		if request.method != http.MethodGet {
			writes = append(writes, request.method+" "+request.path)
		}
	}
	return writes
}

// storedJSON returns one field of a stored filter as JSON.
func (f *fakeAutobrrAPI) storedJSON(t *testing.T, id int, field string) string {
	t.Helper()
	stored, ok := f.filter(strconv.Itoa(id))
	require.True(t, ok, "filter %d is not stored", id)
	data, err := json.Marshal(stored[field])
	require.NoError(t, err)
	return string(data)
}

func setupFilterArgs(autobrrURL string, extra ...string) []string {
	return append([]string{
		"setup", "filter",
		"--autobrr-url", autobrrURL, "--autobrr-api-key", fakeAutobrrKey,
		"--webhook-url", "http://seasonpackarr:42069",
	}, extra...)
}

func TestSetupFilterCommand_CreatesFilterForClient(t *testing.T) {
	isolateCLI(t)
	fake, server := newFakeAutobrrAPI(t)

	code, stdout, stderr := runCLI(t, setupFilterArgs(server.URL, "--client", "tv", "--api", "secret")...)

	require.Equal(t, 0, code, stderr)
	require.Len(t, fake.filters, 1)
	assert.Equal(t, "GET /api/filters", fake.requests[0].method+" "+fake.requests[0].path)
	create := fake.requests[1]
	assert.Equal(t, "POST /api/filters", create.method+" "+create.path)
	assert.Equal(t, map[string]any{
		"name": "seasonpackarr-tv", "enabled": false, "seasons": "1-99", "episodes": "0",
		"resolutions": []any{}, "sources": []any{}, "codecs": []any{}, "containers": []any{},
	}, create.body)
	assert.JSONEq(t, `[
		{
			"name": "1. Candidate check", "index": 1, "type": "WEBHOOK", "enabled": true, "on_error": "REJECT",
			"webhook_host": "http://seasonpackarr:42069/api/candidate", "webhook_method": "POST",
			"webhook_headers": "X-API-Token=secret", "webhook_expect_status": 250,
			"webhook_data": "{\n  \"name\": \"{{ .TorrentName }}\",\n  \"clientname\": \"tv\"\n}"
		},
		{
			"name": "2. Match", "index": 0, "type": "WEBHOOK", "enabled": true, "on_error": "REJECT",
			"webhook_host": "http://seasonpackarr:42069/api/match", "webhook_method": "POST",
			"webhook_headers": "X-API-Token=secret", "webhook_expect_status": 250,
			"webhook_data": "{\n  \"name\": \"{{ .TorrentName }}\",\n  \"torrent\": \"{{ .TorrentDataRawBytes | js }}\",\n  \"clientname\": \"tv\"\n}"
		}
	]`, fake.storedJSON(t, 1, "external"))
	assert.JSONEq(t, `[
		{
			"id": 1, "name": "Import with seasonpackarr", "type": "WEBHOOK", "enabled": true,
			"webhook_type": "JSON", "webhook_method": "POST",
			"webhook_host": "http://seasonpackarr:42069/api/import?apikey=secret",
			"webhook_data": "{\n  \"name\": \"{{ .TorrentName }}\",\n  \"torrent\": \"{{ .TorrentDataRawBytes | js }}\",\n  \"clientname\": \"tv\"\n}"
		}
	]`, fake.storedJSON(t, 1, "actions"))
	// The update sends the stored filter back, so the arrays that the read omits must return.
	index := slices.IndexFunc(fake.requests, func(r capturedRequest) bool { return r.method == http.MethodPut })
	require.GreaterOrEqual(t, index, 0)
	update := fake.requests[index]
	assert.Equal(t, "PUT /api/filters/1", update.method+" "+update.path)
	for _, key := range []string{"resolutions", "sources", "codecs", "containers"} {
		assert.Equal(t, []any{}, update.body[key], key)
	}
	assert.Equal(t, false, update.body["enabled"])
	for _, want := range []string{"Created filter 1 (seasonpackarr-tv) for client tv", "Select the indexers", "priority", "Enable the filter"} {
		assert.Contains(t, stdout, want)
	}
	assert.Empty(t, stderr)
}

func TestSetupFilterCommand_CreatesFilterForEachConfiguredClient(t *testing.T) {
	isolateCLI(t)
	t.Setenv("SEASONPACKARR__DISABLE_CONFIG_FILE", "")
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("apiToken: config-token\nclients:\n  tv: {}\n  anime: {}\n"), 0o600))
	fake, server := newFakeAutobrrAPI(t)

	code, stdout, stderr := runCLI(t, setupFilterArgs(server.URL, "--config", configDir, "--json")...)

	require.Equal(t, 0, code, stderr)
	assert.Empty(t, stderr)
	assert.JSONEq(t, `[
		{"clientname": "anime", "id": 1, "name": "seasonpackarr-anime", "result": "created"},
		{"clientname": "tv", "id": 2, "name": "seasonpackarr-tv", "result": "created"}
	]`, stdout)
	for id, client := range map[int]string{1: "anime", 2: "tv"} {
		external := fake.storedJSON(t, id, "external")
		assert.Contains(t, external, `\"clientname\": \"`+client+`\"`)
		assert.Contains(t, external, `"webhook_headers":"X-API-Token=config-token"`)
		assert.Contains(t, fake.storedJSON(t, id, "actions"), `"webhook_host":"http://seasonpackarr:42069/api/import?apikey=config-token"`)
	}
}

func TestSetupFilterCommand_EmptyTokenWarnsAndOmitsAuth(t *testing.T) {
	isolateCLI(t)
	fake, server := newFakeAutobrrAPI(t)

	code, _, stderr := runCLI(t, setupFilterArgs(server.URL, "--client", "tv")...)

	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stderr, "Warning: the seasonpackarr API token is empty")
	external := fake.storedJSON(t, 1, "external")
	assert.NotContains(t, external, "webhook_headers")
	assert.NotContains(t, external, "X-API-Token")
	assert.Contains(t, fake.storedJSON(t, 1, "actions"), `"webhook_host":"http://seasonpackarr:42069/api/import"`)
}

func TestSetupFilterCommand_DryRunRedactsTokensWithoutAutobrrCalls(t *testing.T) {
	isolateCLI(t)
	fake, server := newFakeAutobrrAPI(t)
	t.Setenv("SEASONPACKARR__AUTOBRR_URL", server.URL)
	t.Setenv("SEASONPACKARR__AUTOBRR_API_KEY", fakeAutobrrKey)

	for _, tt := range []struct {
		name     string
		clearEnv bool
		args     []string
	}{
		{"without autobrr settings", true, []string{"setup", "filter", "--webhook-url", "http://seasonpackarr:42069", "--client", "tv", "--api", "s3cret&key", "--dry-run", "--json"}},
		{"with autobrr settings", false, setupFilterArgs(server.URL, "--client", "tv", "--api", "s3cret&key", "--dry-run", "--json")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.clearEnv {
				t.Setenv("SEASONPACKARR__AUTOBRR_URL", "")
				t.Setenv("SEASONPACKARR__AUTOBRR_API_KEY", "")
			}
			code, stdout, stderr := runCLI(t, tt.args...)

			require.Equal(t, 0, code, stderr)
			assert.NotContains(t, stdout, "s3cret")
			var results []struct {
				Client string         `json:"clientname"`
				Name   string         `json:"name"`
				Result string         `json:"result"`
				Filter map[string]any `json:"filter"`
			}
			require.NoError(t, json.Unmarshal([]byte(stdout), &results), stdout)
			require.Len(t, results, 1)
			assert.Equal(t, "tv", results[0].Client)
			assert.Equal(t, "seasonpackarr-tv", results[0].Name)
			assert.Equal(t, "dry-run", results[0].Result)
			payload, err := json.Marshal(results[0].Filter)
			require.NoError(t, err)
			assert.Contains(t, string(payload), `"webhook_headers":"X-API-Token=[redacted]"`)
			assert.Contains(t, string(payload), `"webhook_host":"http://seasonpackarr:42069/api/import?apikey=[redacted]"`)
			assert.Equal(t, false, results[0].Filter["enabled"])
		})
	}
	assert.Empty(t, fake.requests)
}

func TestSetupFilterCommand_InvalidInputFailsBeforeAutobrrCalls(t *testing.T) {
	isolateCLI(t)
	fake, server := newFakeAutobrrAPI(t)
	base := []string{"setup", "filter", "--client", "tv", "--api", "secret"}
	for _, tt := range []struct {
		name, want string
		args       []string
	}{
		{"missing webhook URL", `required flag(s) "webhook-url" not set`, append(base, "--autobrr-url", server.URL, "--autobrr-api-key", fakeAutobrrKey)},
		{"webhook URL without scheme", "--webhook-url must be an HTTP or HTTPS base URL", setupFilterArgsWith(server.URL, "seasonpackarr:42069", base...)},
		{"webhook URL with other scheme", "--webhook-url must be an HTTP or HTTPS base URL", setupFilterArgsWith(server.URL, "ftp://seasonpackarr:42069", base...)},
		{"webhook URL without host", "--webhook-url must be an HTTP or HTTPS base URL", setupFilterArgsWith(server.URL, "http:///api", base...)},
		{"webhook URL with credentials", "--webhook-url must be an HTTP or HTTPS base URL", setupFilterArgsWith(server.URL, "http://user:pass@seasonpackarr:42069", base...)},
		{"webhook URL with query", "--webhook-url must be an HTTP or HTTPS base URL", setupFilterArgsWith(server.URL, "http://seasonpackarr:42069?apikey=x", base...)},
		{"webhook URL with fragment", "--webhook-url must be an HTTP or HTTPS base URL", setupFilterArgsWith(server.URL, "http://seasonpackarr:42069#top", base...)},
		{"webhook URL with bad port", "--webhook-url port must be from 1 to 65535", setupFilterArgsWith(server.URL, "http://seasonpackarr:70000", base...)},
		{"missing autobrr URL", "--autobrr-url or SEASONPACKARR__AUTOBRR_URL", append(base, "--webhook-url", "http://seasonpackarr:42069", "--autobrr-api-key", fakeAutobrrKey)},
		{"missing autobrr API key", "--autobrr-api-key or SEASONPACKARR__AUTOBRR_API_KEY", append(base, "--webhook-url", "http://seasonpackarr:42069", "--autobrr-url", server.URL)},
		{"invalid autobrr URL", "autobrr URL must be an HTTP or HTTPS base URL", append(base, "--webhook-url", "http://seasonpackarr:42069", "--autobrr-url", "autobrr:7474", "--autobrr-api-key", fakeAutobrrKey)},
		{"no clients", "select a client with --client", setupFilterArgs(server.URL)},
		{"token with header separator", `must not contain ";" or "="`, setupFilterArgsWith(server.URL, "http://seasonpackarr:42069", "setup", "filter", "--client", "tv", "--api", "a;b")},
		{"token with header assignment", `must not contain ";" or "="`, setupFilterArgsWith(server.URL, "http://seasonpackarr:42069", "setup", "filter", "--client", "tv", "--api", "a=b")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, tt.args...)

			assert.Equal(t, 1, code)
			assert.Empty(t, stdout)
			assert.Contains(t, stderr, tt.want)
		})
	}
	assert.Empty(t, fake.requests)
}

func TestSetupFilterCommand_AutobrrErrorsExitWithStatus(t *testing.T) {
	isolateCLI(t)
	t.Run("rejected API key", func(t *testing.T) {
		fake, server := newFakeAutobrrAPI(t)
		args := []string{"setup", "filter", "--autobrr-url", server.URL, "--autobrr-api-key", "wrong-key", "--webhook-url", "http://seasonpackarr:42069", "--client", "tv"}

		code, stdout, stderr := runCLI(t, args...)

		assert.Equal(t, 1, code)
		assert.Empty(t, stdout)
		assert.Contains(t, stderr, "autobrr returned HTTP 401 for GET /api/filters; check --autobrr-api-key")
		assert.NotContains(t, stderr, "wrong-key")
		assert.Empty(t, fake.filters)
	})
	t.Run("failed update after an earlier filter", func(t *testing.T) {
		t.Setenv("SEASONPACKARR__DISABLE_CONFIG_FILE", "")
		configDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("apiToken: config-token\nclients:\n  anime: {}\n  tv: {}\n"), 0o600))
		fake, server := newFakeAutobrrAPI(t)
		fake.failPath = "PUT /api/filters/2"

		code, stdout, stderr := runCLI(t, setupFilterArgs(server.URL, "--config", configDir)...)

		assert.Equal(t, 1, code)
		assert.Contains(t, stdout, "Created filter 1 (seasonpackarr-anime) for client anime")
		assert.NotContains(t, stdout, "client tv")
		assert.Contains(t, stderr, "create the filter for client tv: autobrr returned HTTP 500 for PUT /api/filters/2: internal error")
		assert.Contains(t, stderr, "incomplete filter 2")
	})
	t.Run("unreachable autobrr", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		server.Close()

		code, _, stderr := runCLI(t, setupFilterArgs(server.URL, "--client", "tv")...)

		assert.Equal(t, 1, code)
		assert.Contains(t, stderr, "could not reach autobrr for GET /api/filters")
	})
}

func setupFilterArgsWith(autobrrURL, webhookURL string, base ...string) []string {
	return append(slices.Clone(base), "--autobrr-url", autobrrURL, "--autobrr-api-key", fakeAutobrrKey, "--webhook-url", webhookURL)
}

func TestSetupCommand_PrintsHelp(t *testing.T) {
	isolateCLI(t)

	code, stdout, stderr := runCLI(t, "setup")

	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "filter      Create or update the seasonpackarr filter in autobrr")
	code, stdout, stderr = runCLI(t, "--help")
	require.Equal(t, 0, code, stderr)
	_, tools, found := strings.Cut(stdout, "Tools:")
	require.True(t, found, stdout)
	assert.Contains(t, tools, "setup ")
}

// TestSetupFilterCommand_MatchesAutobrrGuide keeps the manual path in
// docs/AUTOBRR.md and the filter that the command writes the same.
func TestSetupFilterCommand_MatchesAutobrrGuide(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "docs", "AUTOBRR.md"))
	require.NoError(t, err)
	isolateCLI(t)
	// The guide placeholders, with the token as the dry run redacts it.
	guide := strings.ReplaceAll(string(raw), "your-api-token", "[redacted]")
	code, stdout, stderr := runCLI(t, "setup", "filter", "--dry-run", "--json",
		"--webhook-url", "http://seasonpackarr:42069", "--client", "default", "--api", "your-api-token")
	require.Equal(t, 0, code, stderr)
	var results []struct {
		Filter struct {
			External []struct {
				Name                string `json:"name"`
				Index               int    `json:"index"`
				Type                string `json:"type"`
				Enabled             bool   `json:"enabled"`
				OnError             string `json:"on_error"`
				WebhookHost         string `json:"webhook_host"`
				WebhookMethod       string `json:"webhook_method"`
				WebhookHeaders      string `json:"webhook_headers"`
				WebhookExpectStatus int    `json:"webhook_expect_status"`
				WebhookData         string `json:"webhook_data"`
			} `json:"external"`
			Actions []struct {
				Name        string `json:"name"`
				Type        string `json:"type"`
				Enabled     bool   `json:"enabled"`
				WebhookType string `json:"webhook_type"`
				WebhookHost string `json:"webhook_host"`
				WebhookData string `json:"webhook_data"`
			} `json:"actions"`
		} `json:"filter"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &results), stdout)
	require.Len(t, results, 1)
	filter := results[0].Filter
	require.Len(t, filter.External, 2)
	require.Len(t, filter.Actions, 1)

	for i, heading := range []string{"### Entry 1: candidate check", "### Entry 2: match"} {
		fields, data := guideSection(t, guide, heading)
		entry := filter.External[i]
		assert.Equal(t, fields["Name"], entry.Name, heading)
		assert.Equal(t, fields["On Error"], titleCase(entry.OnError), heading)
		assert.Equal(t, fields["Endpoint"], entry.WebhookHost, heading)
		assert.Equal(t, fields["HTTP method"], entry.WebhookMethod, heading)
		assert.Equal(t, fields["HTTP Request Headers"], entry.WebhookHeaders, heading)
		assert.Equal(t, fields["Expected HTTP status code"], strconv.Itoa(entry.WebhookExpectStatus), heading)
		assert.Equal(t, data, entry.WebhookData, heading)
		assert.Equal(t, "WEBHOOK", entry.Type, heading)
		assert.True(t, entry.Enabled, heading)
	}
	// The guide puts the candidate check first, and autobrr runs the highest index first.
	assert.Less(t, strings.Index(guide, "### Entry 1: candidate check"), strings.Index(guide, "### Entry 2: match"))
	assert.Greater(t, filter.External[0].Index, filter.External[1].Index)

	fields, data := guideSection(t, guide, "## Add the import action")
	action := filter.Actions[0]
	assert.Equal(t, fields["Name"], action.Name)
	assert.Equal(t, fields["Endpoint"], action.WebhookHost)
	assert.Equal(t, data, action.WebhookData)
	assert.Contains(t, guide, "set **Action type** to `Webhook`")
	assert.Equal(t, "WEBHOOK", action.Type)
	assert.Contains(t, guide, "**Payload (JSON)**")
	assert.Equal(t, "JSON", action.WebhookType)
	assert.True(t, action.Enabled)
}

// guideSection returns the table fields and the first JSON block of the
// section of guide below heading.
func guideSection(t *testing.T, guide, heading string) (map[string]string, string) {
	t.Helper()
	_, section, found := strings.Cut(guide, "\n"+heading+"\n")
	require.True(t, found, "guide has no heading %q", heading)
	if end := strings.Index(section, "\n#"); end >= 0 {
		section = section[:end]
	}
	fields := map[string]string{}
	for line := range strings.Lines(section) {
		cells := strings.Split(strings.TrimSpace(line), " | ")
		if len(cells) == 2 && strings.HasPrefix(cells[0], "| ") && strings.HasSuffix(cells[1], "` |") {
			fields[strings.TrimPrefix(cells[0], "| ")] = strings.TrimSuffix(strings.TrimPrefix(cells[1], "`"), "` |")
		}
	}
	_, block, found := strings.Cut(section, "```json\n")
	require.True(t, found, "section %q has no JSON block", heading)
	block, _, found = strings.Cut(block, "\n```")
	require.True(t, found, "section %q has an open JSON block", heading)
	return fields, block
}

// titleCase turns an autobrr enum value such as REJECT into its label Reject.
func titleCase(value string) string {
	if value == "" {
		return ""
	}
	return value[:1] + strings.ToLower(value[1:])
}

func TestSetupFilterCommand_EscapesTokenInImportQuery(t *testing.T) {
	isolateCLI(t)
	fake, server := newFakeAutobrrAPI(t)

	code, _, stderr := runCLI(t, setupFilterArgs(server.URL, "--client", "tv", "--api", "a&b c")...)

	require.Equal(t, 0, code, stderr)
	assert.Contains(t, fake.storedJSON(t, 1, "external"), `"webhook_headers":"X-API-Token=a&b c"`)
	assert.Contains(t, fake.storedJSON(t, 1, "actions"), `"webhook_host":"http://seasonpackarr:42069/api/import?apikey=a%26b+c"`)
}

func TestSetupFilterCommand_AcceptsHTTPSWebhookURLWithProxyPath(t *testing.T) {
	isolateCLI(t)
	fake, server := newFakeAutobrrAPI(t)

	code, _, stderr := runCLI(t, setupFilterArgsWith(server.URL, "https://proxy.example/seasonpackarr/", "setup", "filter", "--client", "tv", "--api", "secret")...)

	require.Equal(t, 0, code, stderr)
	external := fake.storedJSON(t, 1, "external")
	assert.Contains(t, external, `"webhook_host":"https://proxy.example/seasonpackarr/api/candidate"`)
	assert.Contains(t, external, `"webhook_host":"https://proxy.example/seasonpackarr/api/match"`)
	assert.Contains(t, fake.storedJSON(t, 1, "actions"), `"webhook_host":"https://proxy.example/seasonpackarr/api/import?apikey=secret"`)
}

// v016Filter is a filter that a user set up by hand for v0.16.0.
const v016Filter = `{
	"name": "TV packs", "enabled": true, "priority": 20, "seasons": "1-30", "episodes": "0",
	"resolutions": ["1080p", "2160p"], "sources": ["WEB-DL"], "codecs": [], "containers": [],
	"indexers": [{"id": 4, "name": "TorrentLeech", "identifier": "torrentleech"}],
	"notifications": [{"notification_id": 2, "events": ["PUSH_APPROVED"]}],
	"external": [{"id": 9, "name": "pack", "index": 0, "type": "WEBHOOK", "enabled": true,
		"webhook_host": "http://seasonpackarr:42069/api/pack", "webhook_expect_status": 200}],
	"actions": [
		{"name": "qBittorrent", "type": "QBITTORRENT", "enabled": true, "client_id": 1},
		{"name": "Old pack webhook", "type": "WEBHOOK", "enabled": true,
			"webhook_host": "http://seasonpackarr:42069/api/pack?apikey=old-secret"}
	]
}`

func TestSetupFilterCommand_UpdatesFilterAndKeepsOtherSettings(t *testing.T) {
	isolateCLI(t)
	fake, server := newFakeAutobrrAPI(t)
	id := fake.seed(t, v016Filter)

	code, stdout, stderr := runCLI(t, setupFilterArgs(server.URL, "--client", "tv", "--api", "secret", "--filter-id", strconv.Itoa(id))...)

	require.Equal(t, 0, code, stderr)
	assert.Empty(t, stderr)
	assert.Equal(t, []string{"DELETE /api/actions/1", "DELETE /api/actions/2", "PUT /api/filters/1"}, fake.writes())
	stored, ok := fake.filter(strconv.Itoa(id))
	require.True(t, ok)
	for key, want := range map[string]any{
		"name": "TV packs", "enabled": true, "priority": float64(20), "seasons": "1-30", "episodes": "0",
		"resolutions": []any{"1080p", "2160p"}, "sources": []any{"WEB-DL"}, "codecs": []any{}, "containers": []any{},
		"indexers":      []any{map[string]any{"id": float64(4), "name": "TorrentLeech", "identifier": "torrentleech"}},
		"notifications": []any{map[string]any{"notification_id": float64(2), "events": []any{"PUSH_APPROVED"}}},
	} {
		assert.Equal(t, want, stored[key], key)
	}
	external := fake.storedJSON(t, id, "external")
	assert.NotContains(t, external, "/api/pack")
	assert.Contains(t, external, `"webhook_host":"http://seasonpackarr:42069/api/candidate"`)
	assert.Contains(t, external, `"webhook_host":"http://seasonpackarr:42069/api/match"`)
	actions := fake.storedJSON(t, id, "actions")
	assert.NotContains(t, actions, "qBittorrent")
	assert.NotContains(t, actions, "/api/pack")
	assert.Contains(t, actions, `"webhook_host":"http://seasonpackarr:42069/api/import?apikey=secret"`)

	assert.Contains(t, stdout, "Updated filter 1 (TV packs) for client tv")
	assert.Contains(t, stdout, "Removed action 1 (qBittorrent, QBITTORRENT)")
	assert.Contains(t, stdout, "Removed action 2 (Old pack webhook, WEBHOOK)")
	assert.NotContains(t, stdout, "old-secret")
	assert.NotContains(t, stdout, "Enable the filter")
}

func TestSetupFilterCommand_UpdateReportsRemovedActionsAsJSON(t *testing.T) {
	isolateCLI(t)
	fake, server := newFakeAutobrrAPI(t)
	id := fake.seed(t, v016Filter)

	code, stdout, stderr := runCLI(t, setupFilterArgs(server.URL, "--client", "tv", "--api", "secret", "--filter-id", strconv.Itoa(id), "--json")...)

	require.Equal(t, 0, code, stderr)
	assert.JSONEq(t, `[{
		"clientname": "tv", "id": 1, "name": "TV packs", "result": "updated",
		"removed_actions": [
			{"id": 1, "name": "qBittorrent", "type": "QBITTORRENT"},
			{"id": 2, "name": "Old pack webhook", "type": "WEBHOOK"}
		]
	}]`, stdout)
}

func TestSetupFilterCommand_RerunUpdateRemovesOnlyImportAction(t *testing.T) {
	isolateCLI(t)
	fake, server := newFakeAutobrrAPI(t)
	id := fake.seed(t, v016Filter)
	code, _, stderr := runCLI(t, setupFilterArgs(server.URL, "--client", "tv", "--api", "secret", "--filter-id", strconv.Itoa(id))...)
	require.Equal(t, 0, code, stderr)

	code, stdout, stderr := runCLI(t, setupFilterArgs(server.URL, "--client", "tv", "--api", "secret", "--filter-id", strconv.Itoa(id), "--json")...)

	require.Equal(t, 0, code, stderr)
	assert.JSONEq(t, `[{
		"clientname": "tv", "id": 1, "name": "TV packs", "result": "updated",
		"removed_actions": [{"id": 3, "name": "Import with seasonpackarr", "type": "WEBHOOK"}]
	}]`, stdout)
	actions := fake.storedJSON(t, id, "actions")
	assert.Equal(t, 1, strings.Count(actions, `"name":"Import with seasonpackarr"`), actions)
}

func TestSetupFilterCommand_FilterIDNeedsExactlyOneClient(t *testing.T) {
	isolateCLI(t)
	t.Setenv("SEASONPACKARR__DISABLE_CONFIG_FILE", "")
	twoClients := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(twoClients, "config.yaml"), []byte("apiToken: config-token\nclients:\n  tv: {}\n  anime: {}\n"), 0o600))
	oneClient := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(oneClient, "config.yaml"), []byte("apiToken: config-token\nclients:\n  tv: {}\n"), 0o600))
	noClients := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(noClients, "config.yaml"), []byte("apiToken: config-token\n"), 0o600))

	for _, tt := range []struct {
		name, want string
		args       []string
	}{
		{"several configured clients", "2 clients are configured; select one with --client", []string{"--config", twoClients, "--filter-id", "1"}},
		{"several configured clients in a dry run", "2 clients are configured; select one with --client", []string{"--config", twoClients, "--filter-id", "1", "--dry-run"}},
		{"no clients", "select a client with --client", []string{"--config", noClients, "--filter-id", "1"}},
		{"zero filter ID", "--filter-id must be a positive filter ID", []string{"--config", oneClient, "--filter-id", "0"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake, server := newFakeAutobrrAPI(t)
			fake.seed(t, v016Filter)

			code, stdout, stderr := runCLI(t, setupFilterArgs(server.URL, tt.args...)...)

			assert.Equal(t, 1, code)
			assert.Empty(t, stdout)
			assert.Contains(t, stderr, tt.want)
			assert.Empty(t, fake.requests)
		})
	}
	t.Run("the only configured client", func(t *testing.T) {
		fake, server := newFakeAutobrrAPI(t)
		id := fake.seed(t, v016Filter)

		code, stdout, stderr := runCLI(t, setupFilterArgs(server.URL, "--config", oneClient, "--filter-id", strconv.Itoa(id))...)

		require.Equal(t, 0, code, stderr)
		assert.Contains(t, stdout, "Updated filter 1 (TV packs) for client tv")
		assert.Contains(t, fake.storedJSON(t, id, "external"), `\"clientname\": \"tv\"`)
	})
	t.Run("one client from --client of several", func(t *testing.T) {
		fake, server := newFakeAutobrrAPI(t)
		id := fake.seed(t, v016Filter)

		code, stdout, stderr := runCLI(t, setupFilterArgs(server.URL, "--config", twoClients, "--client", "anime", "--filter-id", strconv.Itoa(id))...)

		require.Equal(t, 0, code, stderr)
		assert.Contains(t, stdout, "Updated filter 1 (TV packs) for client anime")
		assert.Contains(t, fake.storedJSON(t, id, "external"), `\"clientname\": \"anime\"`)
	})
}

func TestSetupFilterCommand_ExistingFilterNameStopsBeforeAnyWrite(t *testing.T) {
	isolateCLI(t)
	t.Setenv("SEASONPACKARR__DISABLE_CONFIG_FILE", "")
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("apiToken: config-token\nclients:\n  anime: {}\n  tv: {}\n  movies: {}\n"), 0o600))
	fake, server := newFakeAutobrrAPI(t)
	fake.seed(t, `{"name": "TV packs", "actions": []}`)
	tvID := fake.seed(t, `{"name": "seasonpackarr-tv", "actions": []}`)
	moviesID := fake.seed(t, `{"name": "seasonpackarr-movies", "actions": []}`)

	code, stdout, stderr := runCLI(t, setupFilterArgs(server.URL, "--config", configDir)...)

	assert.Equal(t, 1, code)
	assert.Empty(t, stdout)
	assert.Empty(t, fake.writes())
	assert.Contains(t, stderr, "filter "+strconv.Itoa(tvID)+" (seasonpackarr-tv) already exists for client tv; rerun with --filter-id "+strconv.Itoa(tvID)+" --client tv")
	assert.Contains(t, stderr, "filter "+strconv.Itoa(moviesID)+" (seasonpackarr-movies) already exists for client movies; rerun with --filter-id "+strconv.Itoa(moviesID)+" --client movies")
	assert.NotContains(t, stderr, "anime")
}

func TestSetupFilterCommand_DryRunUpdateShowsChangesWithoutWrites(t *testing.T) {
	isolateCLI(t)
	fake, server := newFakeAutobrrAPI(t)
	id := fake.seed(t, v016Filter)
	args := setupFilterArgs(server.URL, "--client", "tv", "--api", "s3cret", "--filter-id", strconv.Itoa(id), "--dry-run")

	code, stdout, stderr := runCLI(t, args...)

	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "Dry run: update filter 1 (TV packs) for client tv, not sent to autobrr")
	assert.Contains(t, stdout, "Would remove action 1 (qBittorrent, QBITTORRENT)")
	assert.Contains(t, stdout, "Would remove action 2 (Old pack webhook, WEBHOOK)")
	assert.Contains(t, stdout, `"webhook_host": "http://seasonpackarr:42069/api/candidate"`)

	code, stdout, stderr = runCLI(t, append(args, "--json")...)

	require.Equal(t, 0, code, stderr)
	assert.NotContains(t, stdout, "s3cret")
	assert.NotContains(t, stdout, "old-secret")
	var results []struct {
		ID             int    `json:"id"`
		Name           string `json:"name"`
		Result         string `json:"result"`
		RemovedActions []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"removed_actions"`
		Filter map[string]any `json:"filter"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &results), stdout)
	require.Len(t, results, 1)
	assert.Equal(t, "dry-run", results[0].Result)
	assert.Equal(t, "TV packs", results[0].Name)
	require.Len(t, results[0].RemovedActions, 2)
	assert.Equal(t, "qBittorrent", results[0].RemovedActions[0].Name)
	// The dry run shows only the parts that the update replaces.
	assert.ElementsMatch(t, []string{"external", "actions"}, slices.Collect(maps.Keys(results[0].Filter)))
	payload, err := json.Marshal(results[0].Filter)
	require.NoError(t, err)
	assert.Contains(t, string(payload), `"webhook_headers":"X-API-Token=[redacted]"`)
	assert.Contains(t, string(payload), `"webhook_host":"http://seasonpackarr:42069/api/import?apikey=[redacted]"`)

	assert.Empty(t, fake.writes())
	assert.Contains(t, fake.storedJSON(t, id, "actions"), "qBittorrent")
}

func TestSetupFilterCommand_DryRunUpdateNeedsAutobrrSettings(t *testing.T) {
	isolateCLI(t)
	t.Setenv("SEASONPACKARR__AUTOBRR_URL", "")
	t.Setenv("SEASONPACKARR__AUTOBRR_API_KEY", "")

	code, stdout, stderr := runCLI(t, "setup", "filter", "--webhook-url", "http://seasonpackarr:42069", "--client", "tv", "--filter-id", "1", "--dry-run")

	assert.Equal(t, 1, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "--autobrr-url or SEASONPACKARR__AUTOBRR_URL")
}

func TestSetupFilterCommand_UpdateErrorsExitWithStatus(t *testing.T) {
	isolateCLI(t)
	t.Run("unknown filter ID", func(t *testing.T) {
		fake, server := newFakeAutobrrAPI(t)

		code, stdout, stderr := runCLI(t, setupFilterArgs(server.URL, "--client", "tv", "--filter-id", "7")...)

		assert.Equal(t, 1, code)
		assert.Empty(t, stdout)
		assert.Contains(t, stderr, "read filter 7: autobrr returned HTTP 404 for GET /api/filters/7; check --filter-id")
		assert.Empty(t, fake.writes())
	})
	t.Run("failed action delete", func(t *testing.T) {
		fake, server := newFakeAutobrrAPI(t)
		id := fake.seed(t, v016Filter)
		fake.failPath = "DELETE /api/actions/1"

		code, stdout, stderr := runCLI(t, setupFilterArgs(server.URL, "--client", "tv", "--api", "secret", "--filter-id", strconv.Itoa(id))...)

		assert.Equal(t, 1, code)
		assert.Empty(t, stdout)
		assert.Contains(t, stderr, "update filter 1 for client tv: autobrr returned HTTP 500 for DELETE /api/actions/1: internal error")
		assert.NotContains(t, stderr, "rerun")
		assert.Contains(t, fake.storedJSON(t, id, "external"), "/api/pack")
		assert.Contains(t, fake.storedJSON(t, id, "actions"), "qBittorrent")
	})
	t.Run("failed update after the action deletes", func(t *testing.T) {
		fake, server := newFakeAutobrrAPI(t)
		id := fake.seed(t, v016Filter)
		fake.failPath = "PUT /api/filters/1"

		code, stdout, stderr := runCLI(t, setupFilterArgs(server.URL, "--client", "tv", "--api", "secret", "--filter-id", strconv.Itoa(id))...)

		assert.Equal(t, 1, code)
		assert.Empty(t, stdout)
		assert.Contains(t, stderr, "update filter 1 for client tv: autobrr returned HTTP 500 for PUT /api/filters/1: internal error")
		assert.Contains(t, stderr, "the filter has no action until you rerun the command")
		// No old download action stays next to the import action.
		assert.Equal(t, "[]", fake.storedJSON(t, id, "actions"))
	})
}

func TestSetupFilterCommand_UpdateFailsWhenAutobrrKeepsOldActions(t *testing.T) {
	isolateCLI(t)
	fake, server := newFakeAutobrrAPI(t)
	id := fake.seed(t, v016Filter)
	// autobrr returns a filter without actions when its action lookup fails, so the command deletes nothing.
	fake.hideActionsOnce = true

	code, stdout, stderr := runCLI(t, setupFilterArgs(server.URL, "--client", "tv", "--api", "secret", "--filter-id", strconv.Itoa(id))...)

	assert.Equal(t, 1, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "update filter 1 for client tv: filter 1 has 3 actions after the update, but the update sends 1")
	assert.Contains(t, stderr, "action 1 (qBittorrent, QBITTORRENT)")
	assert.Contains(t, stderr, "delete the old actions in autobrr")
}

func TestSetupFilterCommand_UpdateWithoutActionsReportsEmptyRemovedActions(t *testing.T) {
	isolateCLI(t)
	fake, server := newFakeAutobrrAPI(t)
	id := fake.seed(t, `{"name": "TV packs", "enabled": true, "actions": []}`)

	code, stdout, stderr := runCLI(t, setupFilterArgs(server.URL, "--client", "tv", "--api", "secret", "--filter-id", strconv.Itoa(id), "--json")...)

	require.Equal(t, 0, code, stderr)
	assert.JSONEq(t, `[{"clientname": "tv", "id": 1, "name": "TV packs", "result": "updated", "removed_actions": []}]`, stdout)
}

// unrelatedWebhookFilter calls a webhook of another service.
const unrelatedWebhookFilter = `{
	"name": "Notify", "enabled": true,
	"external": [{"name": "check", "index": 0, "type": "WEBHOOK", "enabled": true,
		"webhook_host": "http://notifiarr:5454/api/v1/notification/autobrr"}],
	"actions": [{"name": "Notify", "type": "WEBHOOK", "enabled": true, "webhook_host": "http://notifiarr:5454/api/v1/notification/autobrr"}]
}`

func TestSetupFilterCommand_WarnsAboutStaleFiltersWithoutWrites(t *testing.T) {
	isolateCLI(t)
	fake, server := newFakeAutobrrAPI(t)
	staleID := fake.seed(t, v016Filter)
	fake.seed(t, unrelatedWebhookFilter)
	fake.seed(t, `{"name": "Movies", "actions": [{"name": "qBittorrent", "type": "QBITTORRENT", "enabled": true}]}`)
	staleExternal, staleActions := fake.storedJSON(t, staleID, "external"), fake.storedJSON(t, staleID, "actions")

	code, stdout, stderr := runCLI(t, setupFilterArgs(server.URL, "--client", "tv", "--api", "secret")...)

	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "Created filter 4 (seasonpackarr-tv) for client tv")
	assert.Contains(t, stderr, "Warning: filter 1 (TV packs) calls seasonpackarr at /api/pack; update it with --filter-id 1")
	assert.Equal(t, 1, strings.Count(stderr, "Warning:"), stderr)
	assert.NotContains(t, stdout+stderr, "old-secret")
	// The list has no External entries or actions, so the command reads each filter.
	for _, path := range []string{"/api/filters/1", "/api/filters/2", "/api/filters/3"} {
		assert.True(t, slices.ContainsFunc(fake.requests, func(r capturedRequest) bool { return r.method == http.MethodGet && r.path == path }), path)
	}
	assert.Equal(t, []string{"POST /api/filters", "PUT /api/filters/4"}, fake.writes())
	assert.JSONEq(t, staleExternal, fake.storedJSON(t, staleID, "external"))
	assert.JSONEq(t, staleActions, fake.storedJSON(t, staleID, "actions"))
}

func TestSetupFilterCommand_UpdateWarnsAboutOtherStaleFiltersOnly(t *testing.T) {
	isolateCLI(t)
	other := `{
		"name": "Anime packs", "enabled": true,
		"external": [{"name": "check", "index": 0, "type": "WEBHOOK", "enabled": true, "webhook_host": "http://seasonpackarr:42069/api/candidate"}],
		"actions": [{"name": "Import", "type": "WEBHOOK", "enabled": true, "webhook_host": "http://seasonpackarr:42069/api/import?apikey=other-secret"}]
	}`
	for _, tt := range []struct {
		name, result string
		dryRun       bool
	}{
		{"update", "updated", false},
		{"dry run", "dry-run", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake, server := newFakeAutobrrAPI(t)
			id := fake.seed(t, v016Filter)
			otherID := fake.seed(t, other)
			otherActions := fake.storedJSON(t, otherID, "actions")
			args := setupFilterArgs(server.URL, "--client", "tv", "--api", "secret", "--filter-id", strconv.Itoa(id), "--json")
			if tt.dryRun {
				args = append(args, "--dry-run")
			}

			code, stdout, stderr := runCLI(t, args...)

			require.Equal(t, 0, code, stderr)
			assert.Equal(t, "Warning: filter 2 (Anime packs) calls seasonpackarr at /api/candidate, /api/import; update it with --filter-id 2\n", stderr)
			var results []struct {
				ID           int    `json:"id"`
				Result       string `json:"result"`
				StaleFilters []struct {
					ID        int      `json:"id"`
					Name      string   `json:"name"`
					Endpoints []string `json:"endpoints"`
				} `json:"stale_filters"`
			}
			require.NoError(t, json.Unmarshal([]byte(stdout), &results), stdout)
			require.Len(t, results, 1)
			assert.Equal(t, id, results[0].ID)
			assert.Equal(t, tt.result, results[0].Result)
			require.Len(t, results[0].StaleFilters, 1)
			assert.Equal(t, otherID, results[0].StaleFilters[0].ID)
			assert.Equal(t, "Anime packs", results[0].StaleFilters[0].Name)
			assert.Equal(t, []string{"/api/candidate", "/api/import"}, results[0].StaleFilters[0].Endpoints)
			assert.NotContains(t, stdout+stderr, "other-secret")
			for _, write := range fake.writes() {
				assert.NotContains(t, write, "/api/filters/"+strconv.Itoa(otherID))
				assert.NotContains(t, write, "/api/actions/3")
			}
			if tt.dryRun {
				assert.Empty(t, fake.writes())
			}
			assert.JSONEq(t, otherActions, fake.storedJSON(t, otherID, "actions"))
		})
	}
}

func TestSetupFilterCommand_WarnsAboutEachSeasonpackarrEndpoint(t *testing.T) {
	isolateCLI(t)
	fake, server := newFakeAutobrrAPI(t)
	for _, filter := range []string{
		`{"name": "pack action", "actions": [{"name": "a", "type": "WEBHOOK", "webhook_host": "http://seasonpackarr:42069/api/pack"}]}`,
		`{"name": "parse entry", "external": [{"name": "e", "type": "WEBHOOK", "webhook_host": "https://proxy.example/seasonpackarr/api/parse"}]}`,
		`{"name": "candidate entry", "external": [{"name": "e", "type": "WEBHOOK", "webhook_host": "http://seasonpackarr:42069/api/candidate"}]}`,
		`{"name": "match entry", "external": [{"name": "e", "type": "WEBHOOK", "webhook_host": "http://seasonpackarr:42069/api/match"}]}`,
		`{"name": "import action", "actions": [{"name": "a", "type": "WEBHOOK", "webhook_host": "http://seasonpackarr:42069/api/import?apikey=x"}]}`,
	} {
		fake.seed(t, filter)
	}

	code, stdout, stderr := runCLI(t, setupFilterArgs(server.URL, "--client", "tv", "--api", "secret", "--json")...)

	require.Equal(t, 0, code, stderr)
	for _, want := range []string{
		"filter 1 (pack action) calls seasonpackarr at /api/pack;",
		"filter 2 (parse entry) calls seasonpackarr at /api/parse;",
		"filter 3 (candidate entry) calls seasonpackarr at /api/candidate;",
		"filter 4 (match entry) calls seasonpackarr at /api/match;",
		"filter 5 (import action) calls seasonpackarr at /api/import;",
	} {
		assert.Contains(t, stderr, want)
	}
	var results []struct {
		StaleFilters []struct {
			ID int `json:"id"`
		} `json:"stale_filters"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &results), stdout)
	require.Len(t, results, 1)
	assert.Len(t, results[0].StaleFilters, 5)
	assert.Equal(t, []string{"POST /api/filters", "PUT /api/filters/6"}, fake.writes())
}

func TestSetupFilterCommand_FailedStaleFilterReadStopsBeforeAnyWrite(t *testing.T) {
	isolateCLI(t)
	fake, server := newFakeAutobrrAPI(t)
	fake.seed(t, v016Filter)
	fake.seed(t, unrelatedWebhookFilter)
	fake.failPath = "GET /api/filters/2"

	code, stdout, stderr := runCLI(t, setupFilterArgs(server.URL, "--client", "tv", "--api", "secret")...)

	assert.Equal(t, 1, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "search the autobrr filters that call seasonpackarr: read filter 2: autobrr returned HTTP 500 for GET /api/filters/2: internal error")
	assert.Empty(t, fake.writes())
}
