// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package seasonpack

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The candidate check uses the last scan and reads no filesystem: a file that was removed after the scan still makes
// a candidate.
func TestCandidate_AcceptsCompatibleSourceFolderFile(t *testing.T) {
	pack := newPack(t, "FolderCandidate", 2, 0)
	path := pack.writeFolderFile(t, 1)
	cfg := withSourceFolders(newConfig(), pack.folder())
	f := newFixture(pack.client)
	f.scanSourceFolders(t, cfg)
	require.NoError(t, os.Remove(path))

	outcome := f.processor.Candidate(t.Context(), f.client(t, "default", cfg), pack.name)

	require.NoError(t, outcome.Err)
	assert.Equal(t, domain.StatusSuccessfulMatch, outcome.Status)
	assert.Zero(t, pack.client.FileCalls, "the candidate check requests no file details")
	passed := f.log.Events(t).RequireField(t, "client release passed candidate gate", "client_release", path)
	assert.Equal(t, "source_folder", passed["source_kind"])
}

// A file whose name has the episode number of the pack (E00 equals the pack) is the same pack by the compare, but
// only a torrent proves that the client has the pack.
func TestCandidate_SourceFolderFileNeverGivesAlreadyInClient(t *testing.T) {
	pack := newPack(t, "FolderSamePack", 2, 0)
	path := pack.writeFolderFileNamed(t, "FolderSamePack.S01E00.1080p.WEB-DL.H.264-RlsGrp.mkv")
	cfg := withSourceFolders(newConfig(), pack.folder())
	f := newFixture(pack.client)
	f.scanSourceFolders(t, cfg)

	outcome := f.processor.Candidate(t.Context(), f.client(t, "default", cfg), pack.name)

	require.NoError(t, outcome.Err)
	assert.Equal(t, domain.StatusSuccessfulMatch, outcome.Status)
	f.log.Events(t).RequireField(t, "client release passed candidate gate", "client_release", path)
}

func TestCandidate_LogsIncompatibleSourceFolderFile(t *testing.T) {
	pack := newPack(t, "FolderVariant", 2, 0)
	pack.writeFolderFileNamed(t, "FolderVariant.S01E01.2160p.WEB-DL.H.264-RlsGrp.mkv")
	cfg := withSourceFolders(newConfig(), pack.folder())
	f := newFixture(pack.client)
	f.scanSourceFolders(t, cfg)

	outcome := f.processor.Candidate(t.Context(), f.client(t, "default", cfg), pack.name)

	require.Equal(t, domain.StatusNoMatches, outcome.Status)
	mismatch := f.log.Events(t).Require(t, "client release is not compatible")
	assert.Equal(t, "resolution", mismatch["field"])
	assert.Equal(t, "1080p", mismatch["want"])
	assert.Equal(t, "2160p", mismatch["got"])
	assert.Equal(t, "source_folder", mismatch["source_kind"])
}

// Each import needs the client, so a failed client read fails the candidate check also when the source folders have
// files.
func TestCandidate_FailsOnClientReadWithSourceFolderFiles(t *testing.T) {
	pack := newPack(t, "FolderOffline", 1, 0)
	pack.writeFolderFile(t, 1)
	cfg := withSourceFolders(newConfig(), pack.folder())
	f := newFixture(pack.client)
	f.scanSourceFolders(t, cfg)
	pack.client.TorrentsErr = errors.New("client offline")
	// The startup read stored an inventory, so drop it as its expiry would.
	f.store.DiscardClient("default")

	outcome := f.processor.Candidate(t.Context(), f.client(t, "default", cfg), pack.name)

	assert.Equal(t, domain.StatusGetTorrentsError, outcome.Status)
	assert.ErrorContains(t, outcome.Err, "client offline")
	assert.Equal(t, Failed, outcome.Class())
}

// The season lookup gives the torrents before the files, and a file in two source folders of the client, one below
// the other, makes one candidate.
func TestCandidate_ListsTorrentsBeforeSourceFolderFilesOnce(t *testing.T) {
	pack := newPack(t, "FolderOrder", 2, 1)
	file := pack.writeFolderFile(t, 2)
	cfg := withSourceFolders(newConfig(), pack.folder(), filepath.Dir(file))
	f := newFixture(pack.client)
	f.scanSourceFolders(t, cfg)

	outcome := f.processor.Candidate(t.Context(), f.client(t, "default", cfg), pack.name)

	require.NoError(t, outcome.Err)
	var passed []any
	for _, event := range f.log.Events(t) {
		if event["message"] == "client release passed candidate gate" {
			passed = append(passed, event["client_release"])
		}
	}
	assert.Equal(t, []any{pack.episodeRelease(1), file}, passed)
}
