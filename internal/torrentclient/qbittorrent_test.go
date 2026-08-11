// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	stderrors "errors"
	"maps"
	"path/filepath"
	"testing"
	"time"

	"github.com/autobrr/go-qbittorrent"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/stretchr/testify/require"
)

type stubQbitAPI struct {
	addOptions map[string]string
	addBytes   []byte

	categories  map[string]qbittorrent.Category
	defaultSave string
	categoryErr error
	defaultErr  error
	preferences qbittorrent.AppPreferences
	prefsErr    error

	lookupSeq []qbittorrent.Torrent
	lookupIdx int
	lookups   [][]string

	recheckCalls [][]string
	resumeCalls  [][]string
}

func (s *stubQbitAPI) GetTorrents(o qbittorrent.TorrentFilterOptions) ([]qbittorrent.Torrent, error) {
	if len(o.Hashes) == 0 || len(s.lookupSeq) == 0 {
		return nil, nil
	}
	s.lookups = append(s.lookups, append([]string(nil), o.Hashes...))
	idx := s.lookupIdx
	if idx >= len(s.lookupSeq) {
		idx = len(s.lookupSeq) - 1
	}
	s.lookupIdx++
	return []qbittorrent.Torrent{s.lookupSeq[idx]}, nil
}

func (s *stubQbitAPI) GetFilesInformation(string) (*qbittorrent.TorrentFiles, error) {
	return &qbittorrent.TorrentFiles{}, nil
}

func (s *stubQbitAPI) AddTorrentFromMemory(buf []byte, options map[string]string) (*qbittorrent.TorrentAddResponse, error) {
	s.addBytes = append([]byte(nil), buf...)
	s.addOptions = make(map[string]string, len(options))
	maps.Copy(s.addOptions, options)
	return &qbittorrent.TorrentAddResponse{}, nil
}

func (s *stubQbitAPI) GetCategories() (map[string]qbittorrent.Category, error) {
	return s.categories, s.categoryErr
}

func (s *stubQbitAPI) GetDefaultSavePath() (string, error) {
	return s.defaultSave, s.defaultErr
}

func (s *stubQbitAPI) GetAppPreferences() (qbittorrent.AppPreferences, error) {
	return s.preferences, s.prefsErr
}

func (s *stubQbitAPI) Recheck(hashes []string) error {
	s.recheckCalls = append(s.recheckCalls, append([]string(nil), hashes...))
	return nil
}

func (s *stubQbitAPI) Resume(hashes []string) error {
	s.resumeCalls = append(s.resumeCalls, append([]string(nil), hashes...))
	return nil
}

func newTestQbitClient(stub *stubQbitAPI, policy domain.ImportPolicy) *qbitClient {
	return &qbitClient{
		c:              stub,
		policy:         policy,
		findTimeout:    time.Second,
		recheckTimeout: time.Second,
		pollInterval:   time.Millisecond,
	}
}

func TestQbitBuildTorrentAddOptions(t *testing.T) {
	t.Run("omits unset overrides and always adds paused with skip check", func(t *testing.T) {
		q := newTestQbitClient(&stubQbitAPI{}, domain.ImportPolicy{Category: "tv-hd"})
		opts, err := q.buildTorrentAddOptions()
		require.NoError(t, err)

		prepared := opts.Prepare()
		require.Equal(t, "tv-hd", prepared["category"])
		require.Equal(t, "true", prepared["paused"])
		require.Equal(t, "true", prepared["stopped"])
		require.Equal(t, "true", prepared["skip_checking"])
		_, hasSave := prepared["savepath"]
		require.False(t, hasSave)
		_, hasLayout := prepared["contentLayout"]
		require.False(t, hasLayout)
	})

	t.Run("uses explicit content layout", func(t *testing.T) {
		q := newTestQbitClient(&stubQbitAPI{}, domain.ImportPolicy{Category: "tv-hd", ContentLayout: "subfolder"})
		opts, err := q.buildTorrentAddOptions()
		require.NoError(t, err)
		require.Equal(t, string(qbittorrent.ContentLayoutSubfolderCreate), opts.Prepare()["contentLayout"])
	})

	t.Run("sets download path", func(t *testing.T) {
		q := newTestQbitClient(&stubQbitAPI{}, domain.ImportPolicy{Category: "tv-hd", DownloadPath: "/data/incomplete"})
		opts, err := q.buildTorrentAddOptions()
		require.NoError(t, err)
		prepared := opts.Prepare()
		require.Equal(t, "/data/incomplete", prepared["downloadPath"])
		require.Equal(t, "true", prepared["useDownloadPath"])
	})

	t.Run("joins tags", func(t *testing.T) {
		q := newTestQbitClient(&stubQbitAPI{}, domain.ImportPolicy{Category: "tv-hd", Tags: []string{" a ", "", "b"}})
		opts, err := q.buildTorrentAddOptions()
		require.NoError(t, err)
		require.Equal(t, "a,b", opts.Prepare()["tags"])
	})

	t.Run("rejects invalid layout", func(t *testing.T) {
		q := newTestQbitClient(&stubQbitAPI{}, domain.ImportPolicy{Category: "tv-hd", ContentLayout: "bad"})
		_, err := q.buildTorrentAddOptions()
		require.Error(t, err)
		requireImportFailure(t, err, domain.ReasonImportConfigInvalid, domain.FaultInternal)
	})
}

func TestQbitImportDestination(t *testing.T) {
	tests := []struct {
		name        string
		policy      domain.ImportPolicy
		categories  map[string]qbittorrent.Category
		defaultSave string
		categoryErr error
		defaultErr  error
		preferences qbittorrent.AppPreferences
		prefsErr    error
		want        string
		wantErr     bool
		wantReason  domain.Reason
		wantClass   domain.FaultClass
	}{
		{
			name:   "explicit save path wins",
			policy: domain.ImportPolicy{SavePath: "/data/tv-hd"},
			want:   normalizePath("/data/tv-hd"),
		},
		{
			name:       "absolute category save path",
			policy:     domain.ImportPolicy{Category: "tv-hd"},
			categories: map[string]qbittorrent.Category{"tv-hd": {Name: "tv-hd", SavePath: "/data/tv-hd"}},
			preferences: qbittorrent.AppPreferences{
				AutoTmmEnabled: true,
			},
			want: normalizePath("/data/tv-hd"),
		},
		{
			name:        "empty category save path uses implicit category directory",
			policy:      domain.ImportPolicy{Category: "tv-hd"},
			categories:  map[string]qbittorrent.Category{"tv-hd": {Name: "tv-hd", SavePath: ""}},
			defaultSave: "/downloads",
			preferences: qbittorrent.AppPreferences{
				AutoTmmEnabled: true,
			},
			want: normalizePath("/downloads/tv-hd"),
		},
		{
			name:   "implicit subcategory path uses parent category path",
			policy: domain.ImportPolicy{Category: "tv/hd"},
			categories: map[string]qbittorrent.Category{
				"tv":    {Name: "tv", SavePath: "/downloads/television"},
				"tv/hd": {Name: "tv/hd", SavePath: ""},
			},
			preferences: qbittorrent.AppPreferences{
				AutoTmmEnabled: true,
			},
			want: normalizePath("/downloads/television/hd"),
		},
		{
			name:        "relative category save path joined onto default",
			policy:      domain.ImportPolicy{Category: "tv-hd"},
			categories:  map[string]qbittorrent.Category{"tv-hd": {Name: "tv-hd", SavePath: "tv-hd"}},
			defaultSave: "/downloads",
			preferences: qbittorrent.AppPreferences{
				AutoTmmEnabled: true,
			},
			want: normalizePath("/downloads/tv-hd"),
		},
		{
			name:        "manual mode uses default save path",
			policy:      domain.ImportPolicy{Category: "tv-hd"},
			categories:  map[string]qbittorrent.Category{"tv-hd": {Name: "tv-hd", SavePath: "/data/tv-hd"}},
			defaultSave: "/downloads",
			want:        normalizePath("/downloads"),
		},
		{
			name:       "manual mode can use category save path",
			policy:     domain.ImportPolicy{Category: "tv-hd"},
			categories: map[string]qbittorrent.Category{"tv-hd": {Name: "tv-hd", SavePath: "/data/tv-hd"}},
			preferences: qbittorrent.AppPreferences{
				UseCategoryPathsInManualMode: true,
			},
			want: normalizePath("/data/tv-hd"),
		},
		{
			name:        "category read error",
			policy:      domain.ImportPolicy{Category: "tv-hd"},
			categoryErr: stderrors.New("boom"),
			wantErr:     true,
			wantReason:  domain.ReasonImportDestinationFailed,
			wantClass:   domain.FaultDependency,
		},
		{
			name:       "missing category",
			policy:     domain.ImportPolicy{Category: "tv-hd"},
			categories: map[string]qbittorrent.Category{},
			wantErr:    true,
			wantReason: domain.ReasonImportConfigInvalid,
			wantClass:  domain.FaultInternal,
		},
		{
			name:       "empty resolved destination",
			policy:     domain.ImportPolicy{Category: "tv-hd"},
			categories: map[string]qbittorrent.Category{"tv-hd": {Name: "tv-hd", SavePath: ""}},
			wantErr:    true,
			wantReason: domain.ReasonImportConfigInvalid,
			wantClass:  domain.FaultInternal,
		},
		{
			name:       "default save path read error",
			policy:     domain.ImportPolicy{Category: "tv-hd"},
			categories: map[string]qbittorrent.Category{"tv-hd": {Name: "tv-hd", SavePath: "/data/tv-hd"}},
			defaultErr: stderrors.New("boom"),
			wantErr:    true,
			wantReason: domain.ReasonImportDestinationFailed,
			wantClass:  domain.FaultDependency,
		},
		{
			name:       "preference read error",
			policy:     domain.ImportPolicy{Category: "tv-hd", ContentLayout: "subfolder"},
			categories: map[string]qbittorrent.Category{"tv-hd": {Name: "tv-hd", SavePath: "/data/tv-hd"}},
			prefsErr:   stderrors.New("boom"),
			wantErr:    true,
			wantReason: domain.ReasonImportDestinationFailed,
			wantClass:  domain.FaultDependency,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := newTestQbitClient(&stubQbitAPI{
				categories:  tt.categories,
				defaultSave: tt.defaultSave,
				categoryErr: tt.categoryErr,
				defaultErr:  tt.defaultErr,
				preferences: tt.preferences,
				prefsErr:    tt.prefsErr,
			}, tt.policy)

			destination, err := q.ImportDestination()
			if tt.wantErr {
				require.Error(t, err)
				requireImportFailure(t, err, tt.wantReason, tt.wantClass)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, destination.SavePath())
		})
	}
}

func TestQbitImportDestinationUsesConfiguredContentLayout(t *testing.T) {
	q := newTestQbitClient(&stubQbitAPI{}, domain.ImportPolicy{
		SavePath:      "/data/tv-hd",
		ContentLayout: "nosubfolder",
	})

	destination, err := q.ImportDestination()
	require.NoError(t, err)
	require.Equal(t,
		filepath.Join("/data/tv-hd", "Show.S01E01.mkv"),
		destination.TargetPath("Show.S01", "Show.S01E01.mkv"),
	)
}

func TestQbitImportDestinationUsesClientDefaultContentLayout(t *testing.T) {
	q := newTestQbitClient(&stubQbitAPI{
		preferences: qbittorrent.AppPreferences{TorrentContentLayout: "NoSubfolder"},
	}, domain.ImportPolicy{SavePath: "/data/tv-hd"})

	destination, err := q.ImportDestination()
	require.NoError(t, err)
	require.Equal(t,
		filepath.Join("/data/tv-hd", "Show.S01E01.mkv"),
		destination.TargetPath("Show.S01", "Show.S01E01.mkv"),
	)
}

func TestQbitImportRechecksMissingFilesThenResumes(t *testing.T) {
	const hash = "abcdef"
	stub := &stubQbitAPI{
		lookupSeq: []qbittorrent.Torrent{
			{Hash: hash, InfohashV1: hash, State: qbittorrent.TorrentStateMissingFiles},
			{Hash: hash, InfohashV1: hash, State: qbittorrent.TorrentStateCheckingDl},
			{Hash: hash, InfohashV1: hash, State: qbittorrent.TorrentStatePausedDl},
		},
	}
	q := newTestQbitClient(stub, domain.ImportPolicy{Category: "tv-hd", Tags: []string{"seasonpackarr"}})

	err := q.Import(ImportRequest{TorrentBytes: []byte("torrent"), LegacyHash: hash, HasV1: true, SavePath: "/data/tv-hd"})
	require.NoError(t, err)

	require.NotEmpty(t, stub.addBytes)
	require.Equal(t, "true", stub.addOptions["skip_checking"])
	require.Equal(t, "true", stub.addOptions["paused"])
	_, hasSavePath := stub.addOptions["savepath"]
	require.False(t, hasSavePath)
	require.Equal(t, "tv-hd", stub.addOptions["category"])
	require.Equal(t, "seasonpackarr", stub.addOptions["tags"])
	require.Len(t, stub.recheckCalls, 1)
	require.Equal(t, []string{hash}, stub.recheckCalls[0])
	require.Len(t, stub.resumeCalls, 1)
	require.Equal(t, []string{hash}, stub.resumeCalls[0])
}

func TestQbitImportAutomaticManagementOption(t *testing.T) {
	const hash = "abcdef"
	tests := []struct {
		name            string
		policy          domain.ImportPolicy
		wantSavePath    string
		wantSavePresent bool
		wantAutoTMM     string
		wantAutoPresent bool
	}{
		{
			name:   "category only leaves path and management to qbittorrent",
			policy: domain.ImportPolicy{Category: "tv-hd"},
		},
		{
			name:            "explicit save path disables automatic management",
			policy:          domain.ImportPolicy{Category: "tv-hd", SavePath: "/data/tv-hd"},
			wantSavePath:    "/data/tv-hd",
			wantSavePresent: true,
			wantAutoTMM:     "false",
			wantAutoPresent: true,
		},
		{
			name:            "explicit download path pins final path and disables automatic management",
			policy:          domain.ImportPolicy{Category: "tv-hd", DownloadPath: "/data/incomplete"},
			wantSavePath:    "/data/tv-hd",
			wantSavePresent: true,
			wantAutoTMM:     "false",
			wantAutoPresent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubQbitAPI{
				lookupSeq: []qbittorrent.Torrent{{Hash: hash, InfohashV1: hash, State: qbittorrent.TorrentStateDownloading}},
			}
			q := newTestQbitClient(stub, tt.policy)

			err := q.Import(ImportRequest{TorrentBytes: []byte("torrent"), LegacyHash: hash, HasV1: true, SavePath: "/data/tv-hd"})
			require.NoError(t, err)
			gotSavePath, savePresent := stub.addOptions["savepath"]
			require.Equal(t, tt.wantSavePresent, savePresent)
			require.Equal(t, tt.wantSavePath, gotSavePath)
			gotAutoTMM, autoPresent := stub.addOptions["autoTMM"]
			require.Equal(t, tt.wantAutoPresent, autoPresent)
			require.Equal(t, tt.wantAutoTMM, gotAutoTMM)
		})
	}
}

// TestQbitImportWaitsForCheckingToSettle is the regression guard for the bug the
// live partial-pack test surfaced: after a paused skip-check add, qBittorrent
// reports checkingResumeData (with a misleading 100% progress) before flipping
// to missingFiles. waitForTorrent must skip the transient checking state and
// observe missingFiles, so the recheck actually runs.
func TestQbitImportWaitsForCheckingToSettle(t *testing.T) {
	const hash = "abcdef"
	stub := &stubQbitAPI{
		lookupSeq: []qbittorrent.Torrent{
			{Hash: hash, InfohashV1: hash, State: qbittorrent.TorrentStateCheckingResumeData},
			{Hash: hash, InfohashV1: hash, State: qbittorrent.TorrentStateMissingFiles},
			{Hash: hash, InfohashV1: hash, State: qbittorrent.TorrentStateCheckingDl},
			{Hash: hash, InfohashV1: hash, State: qbittorrent.TorrentStatePausedDl},
		},
	}
	q := newTestQbitClient(stub, domain.ImportPolicy{Category: "tv-hd"})

	err := q.Import(ImportRequest{TorrentBytes: []byte("torrent"), LegacyHash: hash, HasV1: true, SavePath: "/data/tv-hd"})
	require.NoError(t, err)
	require.Len(t, stub.recheckCalls, 1, "recheck must run once missingFiles is observed")
	require.Len(t, stub.resumeCalls, 1)
}

func TestQbitImportSkipsResumeWhenAlreadyActive(t *testing.T) {
	const hash = "abcdef"
	stub := &stubQbitAPI{
		lookupSeq: []qbittorrent.Torrent{
			{Hash: hash, InfohashV1: hash, State: qbittorrent.TorrentStateDownloading},
		},
	}
	q := newTestQbitClient(stub, domain.ImportPolicy{Category: "tv-hd"})

	err := q.Import(ImportRequest{TorrentBytes: []byte("torrent"), LegacyHash: hash, HasV1: true, SavePath: "/data/tv-hd"})
	require.NoError(t, err)
	require.Empty(t, stub.recheckCalls)
	require.Empty(t, stub.resumeCalls, "already-active torrent must not be resumed")
}

func TestQbitImportUsesV2HashForPureV2Torrent(t *testing.T) {
	const (
		legacyHash = "1111111111111111111111111111111111111111"
		v2Hash     = "2222222222222222222222222222222222222222222222222222222222222222"
	)
	stub := &stubQbitAPI{
		lookupSeq: []qbittorrent.Torrent{
			{Hash: v2Hash, InfohashV2: v2Hash, State: qbittorrent.TorrentStatePausedDl},
		},
	}
	q := newTestQbitClient(stub, domain.ImportPolicy{SavePath: "/data/tv-hd"})

	err := q.Import(ImportRequest{
		TorrentBytes: []byte("torrent"),
		SavePath:     "/data/tv-hd",
		LegacyHash:   legacyHash,
		V2Hash:       v2Hash,
		HasV1:        false,
	})
	require.NoError(t, err)
	require.NotEmpty(t, stub.lookups)
	require.Equal(t, []string{v2Hash}, stub.lookups[0])
	require.Equal(t, [][]string{{v2Hash}}, stub.resumeCalls)
}
