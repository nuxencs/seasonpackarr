// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func importStageNames(report ImportReport) []ImportStage {
	stages := make([]ImportStage, len(report.Stages))
	for index, stage := range report.Stages {
		stages[index] = stage.Stage
	}
	return stages
}

// capturedRequest records a single decoded Transmission RPC request for assertions.
type capturedRequest struct {
	Method    string
	Arguments map[string]any
	User      string
	Pass      string
	HadAuth   bool
}

// transmissionTestServer starts an httptest.Server that enforces the
// X-Transmission-Session-Id 409 handshake, echoes the request tag (the library
// rejects mismatched tags), routes canned argument payloads by method name, and
// records every request so tests can assert the wire format the adapter produces.
func transmissionTestServer(t *testing.T, responses map[string]string) (*httptest.Server, *[]capturedRequest) {
	t.Helper()
	var mu sync.Mutex
	captured := make([]capturedRequest, 0)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "testsid")
			w.WriteHeader(http.StatusConflict)
			return
		}

		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method    string         `json:"method"`
			Arguments map[string]any `json:"arguments"`
			Tag       int            `json:"tag"`
		}
		_ = json.Unmarshal(body, &req)

		user, pass, hadAuth := r.BasicAuth()
		mu.Lock()
		captured = append(captured, capturedRequest{
			Method:    req.Method,
			Arguments: req.Arguments,
			User:      user,
			Pass:      pass,
			HadAuth:   hadAuth,
		})
		mu.Unlock()

		args, ok := responses[req.Method]
		if !ok {
			http.Error(w, "unexpected method: "+req.Method, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"result":"success","tag":%d,"arguments":%s}`, req.Tag, args)
	}))
	t.Cleanup(srv.Close)
	return srv, &captured
}

// emptySessionResp satisfies the constructor's session-get ping.
const emptySessionResp = `{}`
