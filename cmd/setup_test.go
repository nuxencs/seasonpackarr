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

// fakeAutobrrAPI stores filters like the autobrr REST API: a create stores
// only the base settings, and a read omits the empty arrays.
type fakeAutobrrAPI struct {
	mu       sync.Mutex
	filters  map[int]map[string]any
	nextID   int
	requests []capturedRequest
	// failPath answers requests to this method and path with HTTP 500.
	failPath string
}

func newFakeAutobrrAPI(t *testing.T) (*fakeAutobrrAPI, *httptest.Server) {
	t.Helper()
	f := &fakeAutobrrAPI{filters: map[int]map[string]any{}, nextID: 1}
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
		for key, value := range stored {
			if values, isArray := value.([]any); isArray && len(values) == 0 {
				continue
			}
			read[key] = value
		}
		assert.NoError(t, json.MarshalWrite(w, read))
	})
	mux.HandleFunc("PUT /api/filters/{id}", func(w http.ResponseWriter, r *http.Request) {
		body := f.record(t, r)
		id, err := strconv.Atoi(r.PathValue("id"))
		assert.NoError(t, err)
		f.mu.Lock()
		f.filters[id] = body
		f.mu.Unlock()
		assert.NoError(t, json.MarshalWrite(w, body))
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
	if r.Method != http.MethodGet {
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
	create := fake.requests[0]
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
			"name": "Import with seasonpackarr", "type": "WEBHOOK", "enabled": true,
			"webhook_type": "JSON", "webhook_method": "POST",
			"webhook_host": "http://seasonpackarr:42069/api/import?apikey=secret",
			"webhook_data": "{\n  \"name\": \"{{ .TorrentName }}\",\n  \"torrent\": \"{{ .TorrentDataRawBytes | js }}\",\n  \"clientname\": \"tv\"\n}"
		}
	]`, fake.storedJSON(t, 1, "actions"))
	// The update sends the stored filter back, so the arrays that the read omits must return.
	update := fake.requests[len(fake.requests)-1]
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
		assert.Contains(t, stderr, "autobrr returned HTTP 401 for POST /api/filters; check --autobrr-api-key")
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
		assert.Contains(t, stderr, "could not reach autobrr for POST /api/filters")
	})
}

func setupFilterArgsWith(autobrrURL, webhookURL string, base ...string) []string {
	return append(slices.Clone(base), "--autobrr-url", autobrrURL, "--autobrr-api-key", fakeAutobrrKey, "--webhook-url", webhookURL)
}

func TestSetupCommand_PrintsHelp(t *testing.T) {
	isolateCLI(t)

	code, stdout, stderr := runCLI(t, "setup")

	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "filter      Create the seasonpackarr filter in autobrr")
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
