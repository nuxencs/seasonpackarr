// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package payload

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"text/template"
	"time"
)

const payloadPack = `
	{
	  "name": "{{ .TorrentName }}",
	  "clientname": "{{ .ClientName }}"
	}`

const payloadParse = `
	{
	  "name":"{{ .TorrentName }}",
	  "torrent":"{{ .TorrentDataRawBytes }}",
	  "clientname": "{{ .ClientName }}"
	}`

type packVars struct {
	TorrentName string
	ClientName  string
}

type parseVars struct {
	TorrentName         string
	TorrentDataRawBytes []byte
	ClientName          string
}

func CompilePackPayload(torrentName string, clientName string) (io.Reader, error) {
	var buffer bytes.Buffer

	tmplVars := packVars{
		TorrentName: torrentName,
		ClientName:  clientName,
	}

	tmpl, err := template.New("Request").Parse(payloadPack)
	if err != nil {
		return nil, err
	}

	if err = tmpl.Execute(&buffer, &tmplVars); err != nil {
		return nil, err
	}

	return &buffer, nil
}

func CompileParsePayload(torrentName string, torrentBytes []byte, clientName string) (io.Reader, error) {
	var buffer bytes.Buffer

	tmplVars := parseVars{
		TorrentName:         torrentName,
		TorrentDataRawBytes: torrentBytes,
		ClientName:          clientName,
	}

	tmpl, err := template.New("Request").Parse(payloadParse)
	if err != nil {
		return nil, err
	}

	if err = tmpl.Execute(&buffer, &tmplVars); err != nil {
		return nil, err
	}

	return &buffer, nil
}

func ExecRequest(url string, body io.Reader, apiToken string) error {
	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Token", apiToken)

	c := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	fmt.Printf("Completed the request with the following response: %s\n"+
		"For more details take a look at the logs!", resp.Status)

	return nil
}
