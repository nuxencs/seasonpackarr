// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	stderrors "errors"
	"maps"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

type fakeQbitAPI struct {
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
	stopCalls    [][]string
	resumeCalls  [][]string

	fileMu         sync.Mutex
	filesByHash    map[string]qbittorrent.TorrentFiles
	fileErrByHash  map[string]error
	fileDelay      time.Duration
	activeFileRead int
	maxFileReads   int
}

func (s *fakeQbitAPI) GetTorrents(o qbittorrent.TorrentFilterOptions) ([]qbittorrent.Torrent, error) {
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

func (s *fakeQbitAPI) GetFilesInformation(hash string) (*qbittorrent.TorrentFiles, error) {
	s.fileMu.Lock()
	s.activeFileRead++
	s.maxFileReads = max(s.maxFileReads, s.activeFileRead)
	files := s.filesByHash[hash]
	err := s.fileErrByHash[hash]
	delay := s.fileDelay
	s.fileMu.Unlock()

	time.Sleep(delay)

	s.fileMu.Lock()
	s.activeFileRead--
	s.fileMu.Unlock()
	if err != nil {
		return nil, err
	}
	return &files, nil
}

func (s *fakeQbitAPI) AddTorrentFromMemory(buf []byte, options map[string]string) (*qbittorrent.TorrentAddResponse, error) {
	s.addBytes = append([]byte(nil), buf...)
	s.addOptions = make(map[string]string, len(options))
	maps.Copy(s.addOptions, options)
	return &qbittorrent.TorrentAddResponse{}, nil
}

func (s *fakeQbitAPI) GetCategories() (map[string]qbittorrent.Category, error) {
	return s.categories, s.categoryErr
}

func (s *fakeQbitAPI) GetDefaultSavePath() (string, error) {
	return s.defaultSave, s.defaultErr
}

func (s *fakeQbitAPI) GetAppPreferences() (qbittorrent.AppPreferences, error) {
	return s.preferences, s.prefsErr
}

func (s *fakeQbitAPI) Recheck(hashes []string) error {
	s.recheckCalls = append(s.recheckCalls, append([]string(nil), hashes...))
	return nil
}

func (s *fakeQbitAPI) Stop(hashes []string) error {
	s.stopCalls = append(s.stopCalls, append([]string(nil), hashes...))
	return nil
}

func (s *fakeQbitAPI) Resume(hashes []string) error {
	s.resumeCalls = append(s.resumeCalls, append([]string(nil), hashes...))
	return nil
}

func newTestQbitClient(api *fakeQbitAPI, policy domain.ImportPolicy) *qbitClient {
	return &qbitClient{
		c:            api,
		policy:       policy,
		findTimeout:  time.Second,
		pollInterval: time.Millisecond,
	}
}

func TestQbitGetFiles_UsesBoundedOrderedReads(t *testing.T) {
	t.Parallel()

	api := &fakeQbitAPI{
		filesByHash: map[string]qbittorrent.TorrentFiles{
			"one":   {{Name: "one.mkv", Size: 1}},
			"two":   {{Name: "two.mkv", Size: 2}},
			"three": {{Name: "three.mkv", Size: 3}},
			"five":  {{Name: "five.mkv", Size: 5}},
			"six":   {{Name: "six.mkv", Size: 6}},
		},
		fileErrByHash: map[string]error{"four": stderrors.New("not found")},
		fileDelay:     10 * time.Millisecond,
	}
	client := newTestQbitClient(api, domain.ImportPolicy{})
	hashes := []string{"one", "two", "three", "four", "five", "six"}

	results := client.GetFiles(t.Context(), hashes)
	require.Len(t, results, len(hashes))
	for index, hash := range hashes {
		require.Equal(t, hash, results[index].Hash)
	}
	require.Equal(t, []File{{Name: "one.mkv", Size: 1}}, results[0].Files)
	require.Error(t, results[3].Err)
	require.Equal(t, qbitFileReadWorkers, api.maxFileReads)
}

func TestQbitBuildTorrentAddOptions(t *testing.T) {
	t.Run("adds a complete pack stopped with skip check", func(t *testing.T) {
		q := newTestQbitClient(&fakeQbitAPI{}, domain.ImportPolicy{Category: "tv-hd"})
		prepared, err := q.buildTorrentAddOptions(true)
		require.NoError(t, err)

		require.Equal(t, "tv-hd", prepared["category"])
		require.Equal(t, "true", prepared["paused"])
		require.Equal(t, "true", prepared["stopped"])
		require.Equal(t, "true", prepared["skip_checking"])
		_, hasStopCondition := prepared["stopCondition"]
		require.False(t, hasStopCondition)
		_, hasSave := prepared["savepath"]
		require.False(t, hasSave)
		_, hasLayout := prepared["contentLayout"]
		require.False(t, hasLayout)
	})

	t.Run("adds a partial pack started with a normal check", func(t *testing.T) {
		q := newTestQbitClient(&fakeQbitAPI{}, domain.ImportPolicy{Category: "tv-hd"})
		prepared, err := q.buildTorrentAddOptions(false)
		require.NoError(t, err)

		require.Equal(t, "false", prepared["paused"])
		require.Equal(t, "false", prepared["stopped"])
		require.Equal(t, "None", prepared["stopCondition"])
		_, hasSkip := prepared["skip_checking"]
		require.False(t, hasSkip, "a partial pack must be hash checked")
	})

	t.Run("uses explicit content layout", func(t *testing.T) {
		q := newTestQbitClient(&fakeQbitAPI{}, domain.ImportPolicy{Category: "tv-hd", ContentLayout: "subfolder"})
		prepared, err := q.buildTorrentAddOptions(true)
		require.NoError(t, err)
		require.Equal(t, string(qbittorrent.ContentLayoutSubfolderCreate), prepared["contentLayout"])
	})

	t.Run("sets download path", func(t *testing.T) {
		q := newTestQbitClient(&fakeQbitAPI{}, domain.ImportPolicy{Category: "tv-hd", DownloadPath: "/data/incomplete"})
		prepared, err := q.buildTorrentAddOptions(true)
		require.NoError(t, err)
		require.Equal(t, "/data/incomplete", prepared["downloadPath"])
		require.Equal(t, "true", prepared["useDownloadPath"])
	})

	t.Run("joins tags", func(t *testing.T) {
		q := newTestQbitClient(&fakeQbitAPI{}, domain.ImportPolicy{Category: "tv-hd", Tags: []string{" a ", "", "b"}})
		prepared, err := q.buildTorrentAddOptions(true)
		require.NoError(t, err)
		require.Equal(t, "a,b", prepared["tags"])
	})

	t.Run("rejects invalid layout", func(t *testing.T) {
		q := newTestQbitClient(&fakeQbitAPI{}, domain.ImportPolicy{Category: "tv-hd", ContentLayout: "bad"})
		_, err := q.buildTorrentAddOptions(true)
		require.Error(t, err)
		require.Equal(t, domain.StatusImportConfigError, ImportStatusCode(err))
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
		},
		{
			name:       "missing category",
			policy:     domain.ImportPolicy{Category: "tv-hd"},
			categories: map[string]qbittorrent.Category{},
			wantErr:    true,
		},
		{
			name:       "empty resolved destination",
			policy:     domain.ImportPolicy{Category: "tv-hd"},
			categories: map[string]qbittorrent.Category{"tv-hd": {Name: "tv-hd", SavePath: ""}},
			wantErr:    true,
		},
		{
			name:       "default save path read error",
			policy:     domain.ImportPolicy{Category: "tv-hd"},
			categories: map[string]qbittorrent.Category{"tv-hd": {Name: "tv-hd", SavePath: "/data/tv-hd"}},
			defaultErr: stderrors.New("boom"),
			wantErr:    true,
		},
		{
			name:       "preference read error",
			policy:     domain.ImportPolicy{Category: "tv-hd", ContentLayout: "subfolder"},
			categories: map[string]qbittorrent.Category{"tv-hd": {Name: "tv-hd", SavePath: "/data/tv-hd"}},
			prefsErr:   stderrors.New("boom"),
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := newTestQbitClient(&fakeQbitAPI{
				categories:  tt.categories,
				defaultSave: tt.defaultSave,
				categoryErr: tt.categoryErr,
				defaultErr:  tt.defaultErr,
				preferences: tt.preferences,
				prefsErr:    tt.prefsErr,
			}, tt.policy)

			destination, err := q.ImportDestination(t.Context())
			if tt.wantErr {
				require.Error(t, err)
				require.Equal(t, domain.StatusImportConfigError, ImportStatusCode(err))
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, destination.SavePath())
		})
	}
}

func TestQbitImportDestination_UsesConfiguredContentLayout(t *testing.T) {
	q := newTestQbitClient(&fakeQbitAPI{}, domain.ImportPolicy{
		SavePath:      "/data/tv-hd",
		ContentLayout: "nosubfolder",
	})

	destination, err := q.ImportDestination(t.Context())
	require.NoError(t, err)
	require.Equal(t,
		filepath.Join("/data/tv-hd", "Show.S01E01.mkv"),
		destination.TargetPath("Show.S01", "Show.S01E01.mkv"),
	)
}

func TestQbitImportDestination_UsesClientDefaultContentLayout(t *testing.T) {
	q := newTestQbitClient(&fakeQbitAPI{
		preferences: qbittorrent.AppPreferences{TorrentContentLayout: "NoSubfolder"},
	}, domain.ImportPolicy{SavePath: "/data/tv-hd"})

	destination, err := q.ImportDestination(t.Context())
	require.NoError(t, err)
	require.Equal(t,
		filepath.Join("/data/tv-hd", "Show.S01E01.mkv"),
		destination.TargetPath("Show.S01", "Show.S01E01.mkv"),
	)
}

// TestQbitImport_RechecksMissingFilesWithoutWaiting guards the complete-pack
// fallback: a recheck on a stopped torrent leaves a FilesChecked stop condition
// that only stop clears, and the adapter must not wait for the check.
func TestQbitImport_RechecksMissingFilesWithoutWaiting(t *testing.T) {
	const hash = "abcdef"
	api := &fakeQbitAPI{
		lookupSeq: []qbittorrent.Torrent{
			{Hash: hash, InfohashV1: hash, State: qbittorrent.TorrentStateMissingFiles},
			{Hash: hash, InfohashV1: hash, State: qbittorrent.TorrentStateCheckingDl},
		},
	}
	q := newTestQbitClient(api, domain.ImportPolicy{Category: "tv-hd", Tags: []string{"seasonpackarr"}})

	_, err := q.Import(t.Context(), ImportRequest{TorrentBytes: []byte("torrent"), LegacyHash: hash, HasV1: true, SavePath: "/data/tv-hd", DataComplete: true})
	require.NoError(t, err)

	require.NotEmpty(t, api.addBytes)
	require.Equal(t, "true", api.addOptions["skip_checking"])
	require.Equal(t, "true", api.addOptions["paused"])
	_, hasSavePath := api.addOptions["savepath"]
	require.False(t, hasSavePath)
	require.Equal(t, "tv-hd", api.addOptions["category"])
	require.Equal(t, "seasonpackarr", api.addOptions["tags"])
	require.Equal(t, [][]string{{hash}}, api.recheckCalls)
	require.Equal(t, [][]string{{hash}}, api.stopCalls)
	require.Equal(t, [][]string{{hash}}, api.resumeCalls)
	require.Len(t, api.lookups, 1, "the adapter must not poll the recheck")
}

func TestQbitImport_SetsAutomaticManagementOption(t *testing.T) {
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
			api := &fakeQbitAPI{
				lookupSeq: []qbittorrent.Torrent{{Hash: hash, InfohashV1: hash, State: qbittorrent.TorrentStateDownloading}},
			}
			q := newTestQbitClient(api, tt.policy)

			_, err := q.Import(t.Context(), ImportRequest{TorrentBytes: []byte("torrent"), LegacyHash: hash, HasV1: true, SavePath: "/data/tv-hd"})
			require.NoError(t, err)
			gotSavePath, savePresent := api.addOptions["savepath"]
			require.Equal(t, tt.wantSavePresent, savePresent)
			require.Equal(t, tt.wantSavePath, gotSavePath)
			gotAutoTMM, autoPresent := api.addOptions["autoTMM"]
			require.Equal(t, tt.wantAutoPresent, autoPresent)
			require.Equal(t, tt.wantAutoTMM, gotAutoTMM)
		})
	}
}

// TestQbitImport_WaitsForCheckingToSettle is the regression guard for the bug the
// live partial-pack test surfaced: after a paused skip-check add, qBittorrent
// reports checkingResumeData (with a misleading 100% progress) before flipping
// to missingFiles. waitForTorrent must skip the transient checking state and
// observe missingFiles, so the recheck actually runs.
func TestQbitImport_WaitsForCheckingToSettle(t *testing.T) {
	const hash = "abcdef"
	api := &fakeQbitAPI{
		lookupSeq: []qbittorrent.Torrent{
			{Hash: hash, InfohashV1: hash, State: qbittorrent.TorrentStateCheckingResumeData},
			{Hash: hash, InfohashV1: hash, State: qbittorrent.TorrentStateMissingFiles},
			{Hash: hash, InfohashV1: hash, State: qbittorrent.TorrentStateCheckingDl},
			{Hash: hash, InfohashV1: hash, State: qbittorrent.TorrentStatePausedDl},
		},
	}
	q := newTestQbitClient(api, domain.ImportPolicy{Category: "tv-hd"})

	report, err := q.Import(t.Context(), ImportRequest{TorrentBytes: []byte("torrent"), LegacyHash: hash, HasV1: true, SavePath: "/data/tv-hd", DataComplete: true})
	require.NoError(t, err)
	require.Len(t, api.recheckCalls, 1, "recheck must run once missingFiles is observed")
	require.Len(t, api.resumeCalls, 1)
	require.Equal(t, []ImportStage{
		ImportStageConfig,
		ImportStageAdd,
		ImportStageFind,
		ImportStageRecheck,
		ImportStageResume,
	}, importStageNames(report))
}

// TestQbitImport_PartialPackReturnsWhileChecking is the regression guard for
// the autobrr timeout: a partial pack must not wait for qBittorrent's check,
// which can take minutes. qBittorrent starts the torrent after the check.
func TestQbitImport_PartialPackReturnsWhileChecking(t *testing.T) {
	const hash = "abcdef"
	api := &fakeQbitAPI{
		lookupSeq: []qbittorrent.Torrent{
			{Hash: hash, InfohashV1: hash, State: qbittorrent.TorrentStateCheckingResumeData},
			{Hash: hash, InfohashV1: hash, State: qbittorrent.TorrentStateCheckingDl},
		},
	}
	q := newTestQbitClient(api, domain.ImportPolicy{Category: "tv-hd"})

	report, err := q.Import(t.Context(), ImportRequest{TorrentBytes: []byte("torrent"), LegacyHash: hash, HasV1: true, SavePath: "/data/tv-hd"})
	require.NoError(t, err)
	require.Len(t, api.lookups, 1, "a partial pack must return on first appearance")
	require.Empty(t, api.recheckCalls)
	require.Empty(t, api.resumeCalls)
	_, hasSkip := api.addOptions["skip_checking"]
	require.False(t, hasSkip)
	require.Equal(t, "false", api.addOptions["stopped"])
	require.Equal(t, []ImportStage{
		ImportStageConfig,
		ImportStageAdd,
		ImportStageFind,
	}, importStageNames(report))
}

func TestQbitImport_PartialPackResumesStoppedTorrent(t *testing.T) {
	const hash = "abcdef"
	api := &fakeQbitAPI{
		lookupSeq: []qbittorrent.Torrent{
			{Hash: hash, InfohashV1: hash, State: qbittorrent.TorrentStateStoppedDl},
		},
	}
	q := newTestQbitClient(api, domain.ImportPolicy{Category: "tv-hd"})

	_, err := q.Import(t.Context(), ImportRequest{TorrentBytes: []byte("torrent"), LegacyHash: hash, HasV1: true, SavePath: "/data/tv-hd"})
	require.NoError(t, err)
	require.Equal(t, [][]string{{hash}}, api.resumeCalls)
}

func TestQbitImport_SkipsResumeWhenAlreadyActive(t *testing.T) {
	const hash = "abcdef"
	api := &fakeQbitAPI{
		lookupSeq: []qbittorrent.Torrent{
			{Hash: hash, InfohashV1: hash, State: qbittorrent.TorrentStateDownloading},
		},
	}
	q := newTestQbitClient(api, domain.ImportPolicy{Category: "tv-hd"})

	report, err := q.Import(t.Context(), ImportRequest{TorrentBytes: []byte("torrent"), LegacyHash: hash, HasV1: true, SavePath: "/data/tv-hd"})
	require.NoError(t, err)
	require.Equal(t, []ImportStage{
		ImportStageConfig,
		ImportStageAdd,
		ImportStageFind,
	}, importStageNames(report))
	require.Empty(t, api.recheckCalls)
	require.Empty(t, api.resumeCalls, "already-active torrent must not be resumed")
}

func TestQbitImport_UsesV2HashForPureV2Torrent(t *testing.T) {
	const (
		legacyHash = "1111111111111111111111111111111111111111"
		v2Hash     = "2222222222222222222222222222222222222222222222222222222222222222"
	)
	api := &fakeQbitAPI{
		lookupSeq: []qbittorrent.Torrent{
			{Hash: v2Hash, InfohashV2: v2Hash, State: qbittorrent.TorrentStatePausedDl},
		},
	}
	q := newTestQbitClient(api, domain.ImportPolicy{SavePath: "/data/tv-hd"})

	_, err := q.Import(t.Context(), ImportRequest{
		TorrentBytes: []byte("torrent"),
		SavePath:     "/data/tv-hd",
		LegacyHash:   legacyHash,
		V2Hash:       v2Hash,
		HasV1:        false,
	})
	require.NoError(t, err)
	require.NotEmpty(t, api.lookups)
	require.Equal(t, []string{v2Hash}, api.lookups[0])
	require.Equal(t, [][]string{{v2Hash}}, api.resumeCalls)
}

func TestQbitImport_RejectsMissingHashBeforeAdd(t *testing.T) {
	api := &fakeQbitAPI{}
	q := newTestQbitClient(api, domain.ImportPolicy{SavePath: "/data/tv-hd"})

	report, err := q.Import(t.Context(), ImportRequest{TorrentBytes: []byte("torrent"), HasV1: true, SavePath: "/data/tv-hd"})
	require.Error(t, err)
	require.Equal(t, domain.StatusImportConfigError, ImportStatusCode(err))
	require.Equal(t, []ImportStage{ImportStageConfig}, importStageNames(report))
	require.Empty(t, api.addBytes)
}
