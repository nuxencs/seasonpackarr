// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/autobrr/go-deluge"
	"github.com/stretchr/testify/require"
)

type fakeDelugeAPI struct {
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

type fakeDelugeLabelAPI struct {
	labels    []string
	setLabels []string
	addLabels []string
}

func (s *fakeDelugeLabelAPI) GetLabels(context.Context) ([]string, error) { return s.labels, nil }

func (s *fakeDelugeLabelAPI) SetTorrentLabel(_ context.Context, _, label string) error {
	s.setLabels = append(s.setLabels, label)
	return nil
}

func (s *fakeDelugeLabelAPI) AddLabel(_ context.Context, label string) error {
	s.addLabels = append(s.addLabels, label)
	return nil
}

func (s *fakeDelugeAPI) Connect(context.Context) error { return nil }

func (s *fakeDelugeAPI) SessionState(context.Context) ([]string, error) {
	s.sessionCalls++
	return s.sessionHashes, s.sessionErr
}

func (s *fakeDelugeAPI) TorrentsStatus(_ context.Context, _ deluge.TorrentState, ids []string) (map[string]*deluge.TorrentStatus, error) {
	s.torrentCalls++
	s.gotTorrentIDs = append([]string(nil), ids...)
	return s.torrents, s.torrentsErr
}

func (s *fakeDelugeAPI) TorrentStatus(context.Context, string) (*deluge.TorrentStatus, error) {
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

func (s *fakeDelugeAPI) AddTorrentFile(_ context.Context, name, content string, options *deluge.Options) (string, error) {
	s.addedName = name
	s.addedContent = content
	s.addedOptions = options
	return s.addedHash, s.addErr
}

func (s *fakeDelugeAPI) ResumeTorrents(_ context.Context, ids ...string) error {
	s.resumed = append(s.resumed, ids...)
	return nil
}

func newTestDelugeClient(api *fakeDelugeAPI, policy domain.ImportPolicy) *delugeClient {
	return &delugeClient{
		c:            api,
		policy:       policy,
		pollInterval: time.Millisecond,
	}
}

func TestEnsureDelugeLabel(t *testing.T) {
	t.Parallel()

	plugin := &fakeDelugeLabelAPI{}
	require.NoError(t, ensureDelugeLabel(t.Context(), plugin, "hash", "seasonpackarr"))
	require.Equal(t, []string{"seasonpackarr"}, plugin.addLabels)
	require.Equal(t, []string{"seasonpackarr"}, plugin.setLabels)
}

func TestDelugeImport_AppliesLowercaseLabel(t *testing.T) {
	t.Parallel()

	api := &fakeDelugeAPI{
		addedHash: "returned-hash",
		status:    &deluge.TorrentStatus{State: string(deluge.StateSeeding), Progress: 100},
	}
	plugin := &fakeDelugeLabelAPI{}
	client := newTestDelugeClient(api, domain.ImportPolicy{
		SavePath: "/downloads/tv",
		Tags:     []string{"SeasonPackArr"},
	})
	client.label = func(context.Context) (delugeLabelAPI, error) { return plugin, nil }

	_, err := client.Import(t.Context(), ImportRequest{
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

// newListingDelugeAPI returns a fake daemon with two torrents. Only one of them
// reports files, and they use different save path fields.
func newListingDelugeAPI() *fakeDelugeAPI {
	return &fakeDelugeAPI{
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
}

func TestDelugeGetTorrents(t *testing.T) {
	t.Parallel()

	api := newListingDelugeAPI()
	client := newTestDelugeClient(api, domain.ImportPolicy{})

	torrents, err := client.GetTorrents(t.Context())
	require.NoError(t, err)
	require.Equal(t, []Torrent{
		{Hash: "aaaa", Name: "Show.S01E01", SavePath: "/legacy-downloads"},
		{Hash: "bbbb", Name: "Show.S01E02", SavePath: "/downloads"},
	}, torrents)
	require.Equal(t, 1, api.torrentCalls, "GetTorrents uses one status call")
}

func TestDelugeGetFiles_ReturnsFilesPerRequestedHash(t *testing.T) {
	t.Parallel()

	api := newListingDelugeAPI()
	client := newTestDelugeClient(api, domain.ImportPolicy{})

	results := client.GetFiles(t.Context(), []string{"AAAA", "missing"})
	require.Len(t, results, 2)
	require.NoError(t, results[0].Err)
	require.Equal(t, "AAAA", results[0].Hash)
	require.Equal(t, []File{
		{Name: "Show.S01/Show.S01E01.mkv", Size: 100},
		{Name: "Show.S01/Show.S01E02.mkv", Size: 200},
	}, results[0].Files)
	require.Error(t, results[1].Err)
	require.Equal(t, 1, api.torrentCalls, "GetFiles uses one status call for every hash")
	require.Equal(t, []string{"AAAA", "missing"}, api.gotTorrentIDs)
}

func TestDelugeGetFiles_ExpandsWholeCallError(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("boom")
	client := newTestDelugeClient(&fakeDelugeAPI{torrentsErr: errBoom}, domain.ImportPolicy{})
	results := client.GetFiles(t.Context(), []string{"one", "two"})

	require.Len(t, results, 2)
	for index, hash := range []string{"one", "two"} {
		require.Equal(t, hash, results[index].Hash)
		require.ErrorIs(t, results[index].Err, errBoom)
	}
}

func TestDelugeGetFiles_RejectsEmptyV1Status(t *testing.T) {
	t.Parallel()

	client := newTestDelugeClient(&fakeDelugeAPI{
		torrents: map[string]*deluge.TorrentStatus{"missing": {}},
	}, domain.ImportPolicy{})

	results := client.GetFiles(t.Context(), []string{"missing"})
	require.Len(t, results, 1)
	require.Error(t, results[0].Err)
}

func TestDelugeGetFiles_V1FiltersUnknownAndDuplicateHashes(t *testing.T) {
	t.Parallel()

	api := &fakeDelugeAPI{
		sessionHashes: []string{"known"},
		torrents: map[string]*deluge.TorrentStatus{
			"known": {Hash: "known", Files: []deluge.File{{Path: "episode.mkv", Size: 100}}},
		},
	}
	client := newTestDelugeClient(api, domain.ImportPolicy{})
	client.v1 = true

	results := client.GetFiles(t.Context(), []string{"KNOWN", "known", "missing"})

	require.Len(t, results, 3)
	require.NoError(t, results[0].Err)
	require.NoError(t, results[1].Err)
	require.Error(t, results[2].Err)
	require.Equal(t, 1, api.sessionCalls)
	require.Equal(t, 1, api.torrentCalls)
	require.Equal(t, []string{"KNOWN"}, api.gotTorrentIDs)
}

func TestDelugeImportDestination(t *testing.T) {
	t.Parallel()

	client := newTestDelugeClient(&fakeDelugeAPI{}, domain.ImportPolicy{SavePath: "/downloads/tv"})
	destination, err := client.ImportDestination(t.Context())
	require.NoError(t, err)
	require.Equal(t, "/downloads/tv", destination.SavePath())
	require.Equal(t, "/downloads/tv/Show.S01/Show.S01E01.mkv", destination.TargetPath("Show.S01", "Show.S01E01.mkv"))

	client.policy.SavePath = ""
	_, err = client.ImportDestination(t.Context())
	require.Error(t, err)
	require.Equal(t, domain.StatusImportConfigError, ImportStatusCode(err))
}

// TestDelugeImport_ResumesWithoutWaitingForCheck is the regression guard for
// the autobrr timeout: libtorrent's check can take minutes, so a checking
// torrent counts as started once the resume takes effect.
func TestDelugeImport_ResumesWithoutWaitingForCheck(t *testing.T) {
	t.Parallel()

	api := &fakeDelugeAPI{
		addedHash: "returned-hash",
		statuses: []*deluge.TorrentStatus{
			{State: string(deluge.StatePaused)},
			{State: string(deluge.StateChecking)},
			{State: string(deluge.StateSeeding)},
		},
	}
	client := newTestDelugeClient(api, domain.ImportPolicy{SavePath: "/downloads/tv"})
	torrentBytes := []byte("torrent bytes")

	report, err := client.Import(t.Context(), ImportRequest{
		TorrentBytes: torrentBytes,
		SavePath:     "/downloads/tv",
		LegacyHash:   "legacy-hash",
		HasV1:        true,
	})
	require.NoError(t, err)
	require.Equal(t, []ImportStage{
		ImportStageConfig,
		ImportStageAdd,
		ImportStageResume,
	}, importStageNames(report))
	require.Equal(t, "legacy-hash.torrent", api.addedName)
	require.Equal(t, base64.StdEncoding.EncodeToString(torrentBytes), api.addedContent)
	require.NotNil(t, api.addedOptions)
	require.Equal(t, "/downloads/tv", *api.addedOptions.DownloadLocation)
	require.True(t, *api.addedOptions.AddPaused)
	require.Equal(t, []string{"returned-hash"}, api.resumed)
	require.Equal(t, 2, api.statusAt)
}

func TestDelugeImport_RejectsPureV2Torrent(t *testing.T) {
	t.Parallel()

	client := newTestDelugeClient(&fakeDelugeAPI{}, domain.ImportPolicy{SavePath: "/downloads/tv"})
	_, err := client.Import(t.Context(), ImportRequest{SavePath: "/downloads/tv", V2Hash: "v2-hash"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "v1 or hybrid")
	require.Equal(t, domain.StatusImportConfigError, ImportStatusCode(err))
}

func TestDelugeImport_DoesNotMutateExistingV2Torrent(t *testing.T) {
	t.Parallel()

	api := &fakeDelugeAPI{
		addErr: deluge.RPCError{
			ExceptionType:    "AddTorrentError",
			ExceptionMessage: "Torrent already in session (legacy-hash).",
		},
		status: &deluge.TorrentStatus{State: string(deluge.StateSeeding), Progress: 100},
	}
	client := newTestDelugeClient(api, domain.ImportPolicy{SavePath: "/downloads/tv"})

	_, err := client.Import(t.Context(), ImportRequest{
		TorrentBytes: []byte("torrent bytes"),
		SavePath:     "/downloads/tv",
		LegacyHash:   "legacy-hash",
		HasV1:        true,
	})
	require.NoError(t, err)
	require.Empty(t, api.resumed)
}

func TestDelugeImport_DoesNotMutateExistingV1Torrent(t *testing.T) {
	t.Parallel()

	api := &fakeDelugeAPI{
		status: &deluge.TorrentStatus{State: string(deluge.StateSeeding), Progress: 100},
	}
	client := newTestDelugeClient(api, domain.ImportPolicy{SavePath: "/downloads/tv"})

	_, err := client.Import(t.Context(), ImportRequest{
		TorrentBytes: []byte("torrent bytes"),
		SavePath:     "/downloads/tv",
		LegacyHash:   "legacy-hash",
		HasV1:        true,
	})
	require.NoError(t, err)
	require.Empty(t, api.resumed)
}
