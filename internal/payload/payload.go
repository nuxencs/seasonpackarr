// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package payload

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type candidatePayload struct {
	Name       string `json:"name"`
	ClientName string `json:"clientname"`
}

type torrentPayload struct {
	Name       string `json:"name"`
	Torrent    []byte `json:"torrent"`
	ClientName string `json:"clientname"`
}

func CompileCandidate(torrentName string, clientName string) (io.Reader, error) {
	return compile(candidatePayload{Name: torrentName, ClientName: clientName})
}

func CompilePack(torrentName string, torrentBytes []byte, clientName string) (io.Reader, error) {
	return compile(torrentPayload{Name: torrentName, Torrent: torrentBytes, ClientName: clientName})
}

func CompileParse(torrentName string, torrentBytes []byte, clientName string) (io.Reader, error) {
	return compile(torrentPayload{Name: torrentName, Torrent: torrentBytes, ClientName: clientName})
}

func compile(payload any) (io.Reader, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func Exec(url string, body io.Reader, apiToken string) error {
	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Token", apiToken)
	req.Header.Set("Content-Type", "application/json")

	c := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	fmt.Printf("Completed the request with the following response: %d\n"+
		"For more details take a look at the logs!", resp.StatusCode)

	return nil
}
