// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/require"
)

func isolateCLI(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "SEASONPACKARR__") {
			t.Setenv(key, "")
		}
	}
	t.Setenv("SEASONPACKARR__DISABLE_CONFIG_FILE", "true")
	t.Chdir(t.TempDir())
}

func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := execute(t.Context(), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func writeTorrent(t *testing.T) (string, []byte) {
	t.Helper()
	info, err := bencode.Marshal(metainfo.Info{
		Name: "Series.S01.1080p.WEB-DL-GRP", PieceLength: 16384,
		Pieces: make([]byte, 20),
		Files:  []metainfo.FileInfo{{Path: []string{"Series.S01E01.1080p.WEB-DL-GRP.mkv"}, Length: 1}},
	})
	require.NoError(t, err)
	meta := metainfo.MetaInfo{InfoBytes: info}
	var data bytes.Buffer
	require.NoError(t, meta.Write(&data))
	path := filepath.Join(t.TempDir(), "download.TORRENT")
	require.NoError(t, os.WriteFile(path, data.Bytes(), 0o600))
	return path, data.Bytes()
}
