// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build integration

package torrentclient

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/daemontest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration tests run against real daemons. The import folder must be the
// same path for this process and for the daemon.
const (
	envImportDir = "SEASONPACKARR_TEST_IMPORT_DIR"

	envQbitHost = "SEASONPACKARR_TEST_QBIT_HOST"
	envQbitUser = "SEASONPACKARR_TEST_QBIT_USER"
	envQbitPass = "SEASONPACKARR_TEST_QBIT_PASS"

	envTransmissionHost = "SEASONPACKARR_TEST_TRANSMISSION_HOST"
	envTransmissionUser = "SEASONPACKARR_TEST_TRANSMISSION_USER"
	envTransmissionPass = "SEASONPACKARR_TEST_TRANSMISSION_PASS"

	envDelugeType = "SEASONPACKARR_TEST_DELUGE_TYPE"
	envDelugeHost = "SEASONPACKARR_TEST_DELUGE_HOST"
	envDelugePort = "SEASONPACKARR_TEST_DELUGE_PORT"
	envDelugeUser = "SEASONPACKARR_TEST_DELUGE_USER"
	envDelugePass = "SEASONPACKARR_TEST_DELUGE_PASS"
)

// requireDaemon skips the test unless the import folder and the client's gate
// variable are set. In strict mode it fails the test instead. It returns the
// import folder.
func requireDaemon(t *testing.T, gateKey string) string {
	t.Helper()
	daemontest.RequireSettings(t, gateKey, envImportDir)
	return os.Getenv(envImportDir)
}

func importRequest(pack daemontest.Pack, savePath string, dataComplete bool) ImportRequest {
	return ImportRequest{
		TorrentBytes: pack.Torrent,
		LegacyHash:   pack.Hashes.Legacy,
		V2Hash:       pack.Hashes.V2,
		HasV1:        pack.Hashes.HasV1,
		SavePath:     savePath,
		DataComplete: dataComplete,
	}
}

// assertPartialProgress asserts that the client checked a partial pack to the
// share of present episodes. progress is a fraction from 0 to 1. Episodes fill
// whole pieces, so the expected value is exact up to rounding.
func assertPartialProgress(t *testing.T, progress float64, present int) {
	t.Helper()
	want := float64(present) / daemontest.PackEpisodes
	assert.InDelta(t, want, progress, 0.01, "the check must find exactly the %d present of %d episodes", present, daemontest.PackEpisodes)
}

// requireListedPack stops the test unless the adapter's read path reports the
// imported pack with its save path and every file.
func requireListedPack(t *testing.T, client TorrentClient, pack daemontest.Pack, savePath string) {
	t.Helper()

	listed, err := client.GetTorrents(t.Context())
	require.NoError(t, err)
	var found *Torrent
	for index := range listed {
		if strings.EqualFold(listed[index].Hash, pack.Hashes.Legacy) {
			found = &listed[index]
		}
	}
	require.NotNil(t, found, "GetTorrents does not list %s", pack.Hashes.Legacy)
	require.Equal(t, normalizePath(savePath), normalizePath(found.SavePath))

	results := client.GetFiles(t.Context(), []string{pack.Hashes.Legacy})
	require.Len(t, results, 1)
	require.NoError(t, results[0].Err)
	require.Len(t, results[0].Files, daemontest.PackEpisodes)
	for _, file := range results[0].Files {
		require.True(t, strings.HasPrefix(file.Name, pack.Name+"/"), "file %q is outside the pack folder", file.Name)
	}
}

// requireFileReadLoad sends the adapter's multi-hash read shape to a real
// daemon. One repeated hash isolates read batching and concurrency from
// torrent setup cost. The unknown hash checks partial-result handling.
func requireFileReadLoad(t *testing.T, client TorrentClient, hash string) {
	t.Helper()

	const readCount = 24
	hashes := make([]string, readCount+1)
	for index := range readCount {
		hashes[index] = hash
	}
	hashes[readCount] = strings.Repeat("0", 40)

	started := time.Now()
	results := client.GetFiles(t.Context(), hashes)
	duration := time.Since(started)
	require.Len(t, results, len(hashes))
	for index := range readCount {
		require.Equal(t, hash, results[index].Hash, "result %d", index)
		require.NoError(t, results[index].Err, "result %d", index)
		require.NotEmpty(t, results[index].Files, "result %d", index)
	}
	require.Error(t, results[readCount].Err, "unknown hash must report an error")
	t.Logf("GetFiles returned %d file lists and one unknown-hash error in %s", readCount, duration)
}

// assertDownloadedPack asserts the final state of a download test. Every pack
// file is in the import folder with the source content. Reused episodes keep
// the inode of their source file, and the 1-based episode missing is a new
// file. downloaded is the client's payload byte count for the torrent: only the
// missing episode may download.
func assertDownloadedPack(t *testing.T, importDir string, pack daemontest.Pack, missing int, downloaded int64) {
	t.Helper()
	for index, file := range pack.Episodes {
		sourcePath := filepath.Join(daemontest.SourceDir(importDir), pack.Name, file)
		importPath := filepath.Join(importDir, pack.Name, file)
		importInfo, err := os.Stat(importPath)
		if !assert.NoError(t, err, "pack file %s is not in the import folder", file) {
			continue
		}
		sourceInfo, err := os.Stat(sourcePath)
		if !assert.NoError(t, err, "source file %s", file) {
			continue
		}

		source, err := os.ReadFile(sourcePath)
		if !assert.NoError(t, err, "source file %s", file) {
			continue
		}
		imported, err := os.ReadFile(importPath)
		if !assert.NoError(t, err, "pack file %s", file) {
			continue
		}
		// bytes.Equal, because a diff of two 1 MiB files is unreadable.
		assert.True(t, bytes.Equal(source, imported), "content of %s does not match the source", file)

		if index+1 == missing {
			assert.False(t, os.SameFile(sourceInfo, importInfo), "downloaded episode %s must be a new file", file)
		} else {
			assert.True(t, os.SameFile(sourceInfo, importInfo), "reused episode %s must keep the inode of its source", file)
		}
	}
	// Episodes fill whole pieces, so the missing episode is exactly its size.
	assert.Equal(t, int64(daemontest.PackEpisodeSize), downloaded, "only the missing episode may download")
}
