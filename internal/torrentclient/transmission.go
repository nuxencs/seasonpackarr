// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/pkg/errors"
)

type protoKind int

const (
	protoLegacy   protoKind = iota
	protoJSONRPC2           // Transmission 4.1+ JSON-RPC 2.0 / snake_case
)

type transmissionClient struct {
	url       string
	user      string
	pass      string
	sessionID string
	proto     protoKind
	idCounter uint64
	http      *http.Client
	mu        sync.Mutex
}

func newTransmissionClient(client *domain.Client) (*transmissionClient, error) {
	rpcURL, err := buildTransmissionURL(client)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build transmission url")
	}

	tc := &transmissionClient{
		url:  rpcURL,
		user: client.Username,
		pass: client.Password,
		http: &http.Client{Timeout: 30 * time.Second},
	}

	// Ping to fail fast on bad host/auth and negotiate protocol version.
	var sessionResp struct {
		RPCVersionSemver string `json:"rpc-version-semver"`
	}
	if err := tc.doLegacy("session-get", nil, &sessionResp); err != nil {
		return nil, errors.Wrap(err, "failed to connect to transmission")
	}

	if majorVersion(sessionResp.RPCVersionSemver) >= 6 {
		tc.proto = protoJSONRPC2
	}

	return tc, nil
}

func buildTransmissionURL(client *domain.Client) (string, error) {
	if len(client.Host) == 0 {
		return "", errors.New("host is required")
	}

	host := client.Host
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}

	parsed, err := url.Parse(host)
	if err != nil {
		return "", errors.Wrap(err, "failed to parse host")
	}

	if client.Port != 0 {
		parsed.Host = net.JoinHostPort(parsed.Hostname(), strconv.Itoa(client.Port))
	}

	parsed.Path = "/transmission/rpc"
	return parsed.String(), nil
}

// majorVersion parses the major component from a semver string (e.g. "6.0.0" → 6).
// Returns 0 on any parse failure so unknown versions fall back to legacy.
func majorVersion(semver string) int {
	if semver == "" {
		return 0
	}
	major, _, _ := strings.Cut(semver, ".")
	n, err := strconv.Atoi(major)
	if err != nil {
		return 0
	}
	return n
}

func (t *transmissionClient) post(body []byte, out any) error {
	return t.postOnce(body, out, true)
}

func (t *transmissionClient) postOnce(body []byte, out any, retry bool) error {
	req, err := http.NewRequest(http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return errors.Wrap(err, "failed to create request")
	}

	req.Header.Set("Content-Type", "application/json")

	t.mu.Lock()
	sid := t.sessionID
	t.mu.Unlock()

	if sid != "" {
		req.Header.Set("X-Transmission-Session-Id", sid)
	}

	if t.user != "" {
		req.SetBasicAuth(t.user, t.pass)
	}

	resp, err := t.http.Do(req)
	if err != nil {
		return errors.Wrap(err, "failed to send request")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		newSID := resp.Header.Get("X-Transmission-Session-Id")
		if newSID == "" {
			return errors.New("transmission returned 409 without session id header")
		}

		t.mu.Lock()
		t.sessionID = newSID
		t.mu.Unlock()

		if retry {
			return t.postOnce(body, out, false)
		}

		return errors.New("transmission session id retry failed")
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("transmission rpc: unexpected status %d", resp.StatusCode)
	}

	if out == nil {
		return nil
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return errors.Wrap(err, "failed to read response body")
	}

	if err := json.Unmarshal(data, out); err != nil {
		return errors.Wrap(err, "failed to decode response")
	}

	return nil
}

type legacyEnvelope struct {
	Method    string `json:"method"`
	Arguments any    `json:"arguments,omitempty"`
}

type legacyResponse struct {
	Result    string          `json:"result"`
	Arguments json.RawMessage `json:"arguments"`
}

func (t *transmissionClient) doLegacy(method string, args any, out any) error {
	env := legacyEnvelope{Method: method, Arguments: args}

	body, err := json.Marshal(env)
	if err != nil {
		return errors.Wrap(err, "failed to marshal request")
	}

	var raw legacyResponse
	if err := t.post(body, &raw); err != nil {
		return err
	}

	if raw.Result != "success" {
		return fmt.Errorf("transmission rpc: %s", raw.Result)
	}

	if out != nil && raw.Arguments != nil {
		if err := json.Unmarshal(raw.Arguments, out); err != nil {
			return errors.Wrap(err, "failed to decode arguments")
		}
	}

	return nil
}

type jsonrpcEnvelope struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (t *transmissionClient) doJSONRPC(method string, params any, out any) error {
	id := atomic.AddUint64(&t.idCounter, 1)
	env := jsonrpcEnvelope{JSONRPC: "2.0", ID: id, Method: method, Params: params}

	body, err := json.Marshal(env)
	if err != nil {
		return errors.Wrap(err, "failed to marshal request")
	}

	var raw jsonrpcResponse
	if err := t.post(body, &raw); err != nil {
		return err
	}

	if raw.Error != nil {
		return fmt.Errorf("transmission rpc: %s (code %d)", raw.Error.Message, raw.Error.Code)
	}

	if out != nil && raw.Result != nil {
		if err := json.Unmarshal(raw.Result, out); err != nil {
			return errors.Wrap(err, "failed to decode result")
		}
	}

	return nil
}

type legacyTorrentsArgs struct {
	Fields []string `json:"fields"`
	IDs    []string `json:"ids,omitempty"`
}

type legacyTorrentsResp struct {
	Torrents []struct {
		HashString  string `json:"hashString"`
		Name        string `json:"name"`
		DownloadDir string `json:"downloadDir"`
	} `json:"torrents"`
}

type legacyFilesResp struct {
	Torrents []struct {
		Files []struct {
			Name   string `json:"name"`
			Length int64  `json:"length"`
		} `json:"files"`
	} `json:"torrents"`
}

type jsonrpcTorrentsParams struct {
	Fields []string `json:"fields"`
	IDs    []string `json:"ids,omitempty"`
}

type jsonrpcTorrentsResp struct {
	Torrents []struct {
		HashString  string `json:"hash_string"`
		Name        string `json:"name"`
		DownloadDir string `json:"download_dir"`
	} `json:"torrents"`
}

type jsonrpcFilesResp struct {
	Torrents []struct {
		Files []struct {
			Name   string `json:"name"`
			Length int64  `json:"length"`
		} `json:"files"`
	} `json:"torrents"`
}

func (t *transmissionClient) GetTorrents() ([]Torrent, error) {
	if t.proto == protoJSONRPC2 {
		params := jsonrpcTorrentsParams{Fields: []string{"hash_string", "name", "download_dir"}}

		var resp jsonrpcTorrentsResp
		if err := t.doJSONRPC("torrent_get", params, &resp); err != nil {
			return nil, errors.Wrap(err, "failed to get torrents")
		}

		torrents := make([]Torrent, 0, len(resp.Torrents))
		for _, tr := range resp.Torrents {
			torrents = append(torrents, Torrent{
				Hash:     tr.HashString,
				Name:     tr.Name,
				SavePath: tr.DownloadDir,
			})
		}

		return torrents, nil
	}

	args := legacyTorrentsArgs{Fields: []string{"hashString", "name", "downloadDir"}}

	var resp legacyTorrentsResp
	if err := t.doLegacy("torrent-get", args, &resp); err != nil {
		return nil, errors.Wrap(err, "failed to get torrents")
	}

	torrents := make([]Torrent, 0, len(resp.Torrents))
	for _, tr := range resp.Torrents {
		torrents = append(torrents, Torrent{
			Hash:     tr.HashString,
			Name:     tr.Name,
			SavePath: tr.DownloadDir,
		})
	}

	return torrents, nil
}

func (t *transmissionClient) GetFiles(hash string) ([]File, error) {
	if t.proto == protoJSONRPC2 {
		params := jsonrpcTorrentsParams{
			IDs:    []string{hash},
			Fields: []string{"files"},
		}

		var resp jsonrpcFilesResp
		if err := t.doJSONRPC("torrent_get", params, &resp); err != nil {
			return nil, errors.Wrap(err, "failed to get files")
		}

		if len(resp.Torrents) == 0 {
			return nil, fmt.Errorf("transmission: torrent not found: %s", hash)
		}

		rawFiles := resp.Torrents[0].Files
		files := make([]File, 0, len(rawFiles))
		for _, f := range rawFiles {
			files = append(files, File{
				Name: f.Name,
				Size: f.Length,
			})
		}

		return files, nil
	}

	args := legacyTorrentsArgs{
		IDs:    []string{hash},
		Fields: []string{"files"},
	}

	var resp legacyFilesResp
	if err := t.doLegacy("torrent-get", args, &resp); err != nil {
		return nil, errors.Wrap(err, "failed to get files")
	}

	if len(resp.Torrents) == 0 {
		return nil, fmt.Errorf("transmission: torrent not found: %s", hash)
	}

	rawFiles := resp.Torrents[0].Files
	files := make([]File, 0, len(rawFiles))
	for _, f := range rawFiles {
		files = append(files, File{
			Name: f.Name,
			Size: f.Length,
		})
	}

	return files, nil
}
