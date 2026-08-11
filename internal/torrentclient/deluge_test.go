// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/autobrr/go-deluge"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/stretchr/testify/require"
)

type stubDelugeAPI struct {
	torrents map[string]*deluge.TorrentStatus
	status   *deluge.TorrentStatus
	statuses []*deluge.TorrentStatus
	statusAt int

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

func (s *stubDelugeAPI) TorrentsStatus(context.Context, deluge.TorrentState, []string) (map[string]*deluge.TorrentStatus, error) {
	return s.torrents, nil
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
			"aaaa": {Name: "Show.S01E01", SavePath: "/legacy-downloads"},
		},
		status: &deluge.TorrentStatus{Files: []deluge.File{
			{Path: "Show.S01/Show.S01E01.mkv", Size: 100},
			{Path: "Show.S01/Show.S01E02.mkv", Size: 200},
		}},
	}
	client := newTestDelugeClient(stub, domain.ImportPolicy{})

	torrents, err := client.GetTorrents()
	require.NoError(t, err)
	require.Equal(t, []Torrent{
		{Hash: "aaaa", Name: "Show.S01E01", SavePath: "/legacy-downloads"},
		{Hash: "bbbb", Name: "Show.S01E02", SavePath: "/downloads"},
	}, torrents)

	files, err := client.GetFiles("aaaa")
	require.NoError(t, err)
	require.Equal(t, []File{
		{Name: "Show.S01/Show.S01E01.mkv", Size: 100},
		{Name: "Show.S01/Show.S01E02.mkv", Size: 200},
	}, files)
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
	requireImportFailure(t, err, domain.ReasonImportConfigInvalid, domain.FaultInternal)
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
	requireImportFailure(t, err, domain.ReasonTorrentUnsupported, domain.FaultRequest)
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
