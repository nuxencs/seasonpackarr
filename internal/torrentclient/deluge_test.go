// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/autobrr/go-deluge"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/stretchr/testify/require"
)

type stubDelugeAPI struct {
	torrents      map[string]*deluge.TorrentStatus
	torrentsErr   error
	torrentCalls  int
	gotTorrentIDs []string
	sessionHashes []string
	sessionErr    error
	sessionCalls  int
	status        *deluge.TorrentStatus
	statuses      []*deluge.TorrentStatus
	statusAt      int

	addedName    string
	addedContent string
	addedOptions *deluge.Options
	addedHash    string
	addErr       error
	resumed      []string
}

type stubDelugeLabelAPI struct {
	labels    []string
	setLabels []string
	addLabels []string
}

func (s *stubDelugeLabelAPI) GetLabels(context.Context) ([]string, error) { return s.labels, nil }

func (s *stubDelugeLabelAPI) SetTorrentLabel(_ context.Context, _, label string) error {
	s.setLabels = append(s.setLabels, label)
	return nil
}

func (s *stubDelugeLabelAPI) AddLabel(_ context.Context, label string) error {
	s.addLabels = append(s.addLabels, label)
	return nil
}

func (s *stubDelugeAPI) Connect(context.Context) error { return nil }

func (s *stubDelugeAPI) SessionState(context.Context) ([]string, error) {
	s.sessionCalls++
	return s.sessionHashes, s.sessionErr
}

func (s *stubDelugeAPI) TorrentsStatus(_ context.Context, _ deluge.TorrentState, ids []string) (map[string]*deluge.TorrentStatus, error) {
	s.torrentCalls++
	s.gotTorrentIDs = append([]string(nil), ids...)
	return s.torrents, s.torrentsErr
}

func (s *stubDelugeAPI) TorrentStatus(context.Context, string) (*deluge.TorrentStatus, error) {
	if len(s.statuses) == 0 {
		return s.status, nil
	}
	index := s.statusAt
	if index >= len(s.statuses) {
		index = len(s.statuses) - 1
	}
	s.statusAt++
	return s.statuses[index], nil
}

func (s *stubDelugeAPI) AddTorrentFile(_ context.Context, name, content string, options *deluge.Options) (string, error) {
	s.addedName = name
	s.addedContent = content
	s.addedOptions = options
	return s.addedHash, s.addErr
}

func (s *stubDelugeAPI) ResumeTorrents(_ context.Context, ids ...string) error {
	s.resumed = append(s.resumed, ids...)
	return nil
}

func newTestDelugeClient(stub *stubDelugeAPI, policy domain.ImportPolicy) *delugeClient {
	return &delugeClient{
		c:            stub,
		policy:       policy,
		checkTimeout: 100 * time.Millisecond,
		pollInterval: time.Millisecond,
	}
}

func TestEnsureDelugeLabel(t *testing.T) {
	t.Parallel()

	plugin := &stubDelugeLabelAPI{}
	require.NoError(t, ensureDelugeLabel(context.Background(), plugin, "hash", "seasonpackarr"))
	require.Equal(t, []string{"seasonpackarr"}, plugin.addLabels)
	require.Equal(t, []string{"seasonpackarr"}, plugin.setLabels)
}

func TestDelugeImportAppliesLowercaseLabel(t *testing.T) {
	t.Parallel()

	stub := &stubDelugeAPI{
		addedHash: "returned-hash",
		status:    &deluge.TorrentStatus{State: string(deluge.StateSeeding), Progress: 100},
	}
	plugin := &stubDelugeLabelAPI{}
	client := newTestDelugeClient(stub, domain.ImportPolicy{
		SavePath: "/downloads/tv",
		Tags:     []string{"SeasonPackArr"},
	})
	client.label = func(context.Context) (delugeLabelAPI, error) { return plugin, nil }

	err := client.Import(ImportRequest{
		TorrentBytes: []byte("torrent bytes"),
		SavePath:     "/downloads/tv",
		LegacyHash:   "legacy-hash",
		HasV1:        true,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"seasonpackarr"}, plugin.setLabels)
}

func TestBuildDelugeSettings(t *testing.T) {
	t.Parallel()

	t.Run("uses daemon default port", func(t *testing.T) {
		t.Parallel()
		settings, err := buildDelugeSettings(&domain.Client{
			Host:     "deluge",
			Username: "localclient",
			Password: "secret",
		})
		require.NoError(t, err)
		require.Equal(t, "deluge", settings.Hostname)
		require.Equal(t, uint(delugeDefaultPort), settings.Port)
		require.Equal(t, "localclient", settings.Login)
		require.Equal(t, "secret", settings.Password)
	})

	t.Run("uses configured port", func(t *testing.T) {
		t.Parallel()
		settings, err := buildDelugeSettings(&domain.Client{Host: "192.0.2.1", Port: 60000})
		require.NoError(t, err)
		require.Equal(t, uint(60000), settings.Port)
	})

	for _, client := range []*domain.Client{
		{},
		{Host: "https://deluge.example.com"},
		{Host: "deluge", Port: 65536},
	} {
		_, err := buildDelugeSettings(client)
		require.Error(t, err)
	}
}

func TestDelugeGetTorrentsAndFiles(t *testing.T) {
	t.Parallel()

	stub := &stubDelugeAPI{
		torrents: map[string]*deluge.TorrentStatus{
			"bbbb": {Hash: "bbbb", Name: "Show.S01E02", DownloadLocation: "/downloads"},
			"aaaa": {
				Hash:     "aaaa",
				Name:     "Show.S01E01",
				SavePath: "/legacy-downloads",
				Files: []deluge.File{
					{Path: "Show.S01/Show.S01E01.mkv", Size: 100},
					{Path: "Show.S01/Show.S01E02.mkv", Size: 200},
				},
			},
		},
	}
	client := newTestDelugeClient(stub, domain.ImportPolicy{})

	torrents, err := client.GetTorrents()
	require.NoError(t, err)
	require.Equal(t, []Torrent{
		{Hash: "aaaa", Name: "Show.S01E01", SavePath: "/legacy-downloads"},
		{Hash: "bbbb", Name: "Show.S01E02", SavePath: "/downloads"},
	}, torrents)

	results := client.GetFiles([]string{"AAAA", "missing"})
	require.Len(t, results, 2)
	require.NoError(t, results[0].Err)
	require.Equal(t, "AAAA", results[0].Hash)
	require.Equal(t, []File{
		{Name: "Show.S01/Show.S01E01.mkv", Size: 100},
		{Name: "Show.S01/Show.S01E02.mkv", Size: 200},
	}, results[0].Files)
	require.Error(t, results[1].Err)
	require.Equal(t, 2, stub.torrentCalls, "GetTorrents and GetFiles each use one status call")
	require.Equal(t, []string{"AAAA", "missing"}, stub.gotTorrentIDs)
}

func TestDelugeGetFilesExpandsWholeCallError(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("boom")
	client := newTestDelugeClient(&stubDelugeAPI{torrentsErr: errBoom}, domain.ImportPolicy{})
	results := client.GetFiles([]string{"one", "two"})

	require.Len(t, results, 2)
	for index, hash := range []string{"one", "two"} {
		require.Equal(t, hash, results[index].Hash)
		require.ErrorIs(t, results[index].Err, errBoom)
	}
}

func TestDelugeGetFilesRejectsEmptyV1Status(t *testing.T) {
	t.Parallel()

	client := newTestDelugeClient(&stubDelugeAPI{
		torrents: map[string]*deluge.TorrentStatus{"missing": {}},
	}, domain.ImportPolicy{})

	results := client.GetFiles([]string{"missing"})
	require.Len(t, results, 1)
	require.Error(t, results[0].Err)
}

func TestDelugeV1GetFilesFiltersUnknownAndDuplicateHashes(t *testing.T) {
	t.Parallel()

	stub := &stubDelugeAPI{
		sessionHashes: []string{"known"},
		torrents: map[string]*deluge.TorrentStatus{
			"known": {Hash: "known", Files: []deluge.File{{Path: "episode.mkv", Size: 100}}},
		},
	}
	client := newTestDelugeClient(stub, domain.ImportPolicy{})
	client.v1 = true

	results := client.GetFiles([]string{"KNOWN", "known", "missing"})

	require.Len(t, results, 3)
	require.NoError(t, results[0].Err)
	require.NoError(t, results[1].Err)
	require.Error(t, results[2].Err)
	require.Equal(t, 1, stub.sessionCalls)
	require.Equal(t, 1, stub.torrentCalls)
	require.Equal(t, []string{"KNOWN"}, stub.gotTorrentIDs)
}

func TestDelugeImportDestination(t *testing.T) {
	t.Parallel()

	client := newTestDelugeClient(&stubDelugeAPI{}, domain.ImportPolicy{SavePath: "/downloads/tv"})
	destination, err := client.ImportDestination()
	require.NoError(t, err)
	require.Equal(t, "/downloads/tv", destination.SavePath())
	require.Equal(t, "/downloads/tv/Show.S01/Show.S01E01.mkv", destination.TargetPath("Show.S01", "Show.S01E01.mkv"))

	client.policy.SavePath = ""
	_, err = client.ImportDestination()
	require.Error(t, err)
	require.Equal(t, domain.StatusImportConfigError, ImportStatusCode(err))
}

func TestDelugeImportResumesThenWaitsForCheck(t *testing.T) {
	t.Parallel()

	stub := &stubDelugeAPI{
		addedHash: "returned-hash",
		statuses: []*deluge.TorrentStatus{
			{State: string(deluge.StateChecking)},
			{State: string(deluge.StateSeeding)},
		},
	}
	client := newTestDelugeClient(stub, domain.ImportPolicy{SavePath: "/downloads/tv"})
	torrentBytes := []byte("torrent bytes")

	err := client.Import(ImportRequest{
		TorrentBytes: torrentBytes,
		SavePath:     "/downloads/tv",
		LegacyHash:   "legacy-hash",
		HasV1:        true,
	})
	require.NoError(t, err)
	require.Equal(t, "legacy-hash.torrent", stub.addedName)
	require.Equal(t, base64.StdEncoding.EncodeToString(torrentBytes), stub.addedContent)
	require.NotNil(t, stub.addedOptions)
	require.Equal(t, "/downloads/tv", *stub.addedOptions.DownloadLocation)
	require.True(t, *stub.addedOptions.AddPaused)
	require.Equal(t, []string{"returned-hash"}, stub.resumed)
	require.Equal(t, 2, stub.statusAt)
}

func TestDelugeImportRejectsPureV2Torrent(t *testing.T) {
	t.Parallel()

	client := newTestDelugeClient(&stubDelugeAPI{}, domain.ImportPolicy{SavePath: "/downloads/tv"})
	err := client.Import(ImportRequest{SavePath: "/downloads/tv", V2Hash: "v2-hash"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "v1 or hybrid")
	require.Equal(t, domain.StatusImportConfigError, ImportStatusCode(err))
}

func TestDelugeImportDoesNotMutateExistingV2Torrent(t *testing.T) {
	t.Parallel()

	stub := &stubDelugeAPI{
		addErr: deluge.RPCError{
			ExceptionType:    "AddTorrentError",
			ExceptionMessage: "Torrent already in session (legacy-hash).",
		},
		status: &deluge.TorrentStatus{State: string(deluge.StateSeeding), Progress: 100},
	}
	client := newTestDelugeClient(stub, domain.ImportPolicy{SavePath: "/downloads/tv"})

	err := client.Import(ImportRequest{
		TorrentBytes: []byte("torrent bytes"),
		SavePath:     "/downloads/tv",
		LegacyHash:   "legacy-hash",
		HasV1:        true,
	})
	require.NoError(t, err)
	require.Empty(t, stub.resumed)
}

func TestDelugeImportDoesNotMutateExistingV1Torrent(t *testing.T) {
	t.Parallel()

	stub := &stubDelugeAPI{
		status: &deluge.TorrentStatus{State: string(deluge.StateSeeding), Progress: 100},
	}
	client := newTestDelugeClient(stub, domain.ImportPolicy{SavePath: "/downloads/tv"})

	err := client.Import(ImportRequest{
		TorrentBytes: []byte("torrent bytes"),
		SavePath:     "/downloads/tv",
		LegacyHash:   "legacy-hash",
		HasV1:        true,
	})
	require.NoError(t, err)
	require.Empty(t, stub.resumed)
}
