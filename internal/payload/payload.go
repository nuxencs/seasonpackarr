// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package payload

import (
	"bytes"
	"encoding/json"
	"io"
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

func CompileMatch(torrentName string, torrentBytes []byte, clientName string) (io.Reader, error) {
	return compile(torrentPayload{Name: torrentName, Torrent: torrentBytes, ClientName: clientName})
}

func CompileImport(torrentName string, torrentBytes []byte, clientName string) (io.Reader, error) {
	return compile(torrentPayload{Name: torrentName, Torrent: torrentBytes, ClientName: clientName})
}

func compile(payload any) (io.Reader, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}
