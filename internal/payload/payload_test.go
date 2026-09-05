// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package payload

import (
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func decodePayload(t *testing.T, reader io.Reader) map[string]any {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.NewDecoder(reader).Decode(&payload))
	return payload
}

func TestCompileCandidate_OmitsTorrentBytes(t *testing.T) {
	body, err := CompileCandidate("Series.S01.1080p.WEB-DL-GRP", "default")
	require.NoError(t, err)

	payload := decodePayload(t, body)
	require.Equal(t, "Series.S01.1080p.WEB-DL-GRP", payload["name"])
	require.Equal(t, "default", payload["clientname"])
	require.NotContains(t, payload, "torrent")
}

func TestCompileMatch_IncludesTorrentBytes(t *testing.T) {
	body, err := CompileMatch("Series.S01.1080p.WEB-DL-GRP", []byte("torrent-data"), "default")
	require.NoError(t, err)

	payload := decodePayload(t, body)
	require.Equal(t, "dG9ycmVudC1kYXRh", payload["torrent"])
}
