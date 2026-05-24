// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"bytes"
	"encoding/base64"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/autobrr/go-qbittorrent"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestBuildHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		client  *domain.Client
		want    string
		wantErr bool
	}{
		{
			name:    "empty host",
			client:  &domain.Client{Host: ""},
			want:    "",
			wantErr: true,
		},
		{
			name:   "bare hostname",
			client: &domain.Client{Host: "localhost"},
			want:   "http://localhost",
		},
		{
			name:   "bare hostname with port field",
			client: &domain.Client{Host: "localhost", Port: 8080},
			want:   "http://localhost:8080",
		},
		{
			name:   "hostname with http scheme",
			client: &domain.Client{Host: "http://localhost"},
			want:   "http://localhost",
		},
		{
			name:   "hostname with https scheme",
			client: &domain.Client{Host: "https://myhost"},
			want:   "https://myhost",
		},
		{
			name:   "hostname with scheme and port field",
			client: &domain.Client{Host: "https://myhost", Port: 8080},
			want:   "https://myhost:8080",
		},
		{
			name:   "hostname with existing port and port field override",
			client: &domain.Client{Host: "http://localhost:9090", Port: 8080},
			want:   "http://localhost:8080",
		},
		{
			name:   "schemeless host:port string",
			client: &domain.Client{Host: "localhost:9090"},
			want:   "http://localhost:9090",
		},
		{
			name:   "schemeless host:port with port field override",
			client: &domain.Client{Host: "localhost:9090", Port: 8080},
			want:   "http://localhost:8080",
		},
		{
			name:   "ip address",
			client: &domain.Client{Host: "192.168.1.1"},
			want:   "http://192.168.1.1",
		},
		{
			name:   "ip address with port field",
			client: &domain.Client{Host: "192.168.1.1", Port: 8080},
			want:   "http://192.168.1.1:8080",
		},
		{
			name:   "scheme with ip and port in host",
			client: &domain.Client{Host: "http://192.168.1.1:9090"},
			want:   "http://192.168.1.1:9090",
		},
		{
			name:   "zero port field does not append port",
			client: &domain.Client{Host: "http://localhost", Port: 0},
			want:   "http://localhost",
		},
		{
			name:   "host with path preserved",
			client: &domain.Client{Host: "http://localhost:8080/123456abcdef"},
			want:   "http://localhost:8080/123456abcdef",
		},
		{
			name:   "host with path and port field override",
			client: &domain.Client{Host: "http://localhost:8080/123456abcdef", Port: 9090},
			want:   "http://localhost:9090/123456abcdef",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := buildHost(tt.client)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildHost() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("buildHost() = %q, want %q", got, tt.want)
			}
		})
	}
}

type mockQbitClient struct {
	allTorrents    []qbittorrent.Torrent
	filesByHash    map[string]*qbittorrent.TorrentFiles
	categories     map[string]qbittorrent.Category
	defaultSave    string
	categoryErr    error
	defaultSaveErr error
	lookupSequence []qbittorrent.Torrent
	lookupIndex    int
	addOptions     map[string]string
	addBytes       []byte
	recheckCalls   [][]string
	resumeCalls    [][]string
}

func (m *mockQbitClient) GetTorrents(opts qbittorrent.TorrentFilterOptions) ([]qbittorrent.Torrent, error) {
	if len(opts.Hashes) == 0 {
		return m.allTorrents, nil
	}

	if len(m.lookupSequence) == 0 {
		return []qbittorrent.Torrent{}, nil
	}

	idx := m.lookupIndex
	if idx >= len(m.lookupSequence) {
		idx = len(m.lookupSequence) - 1
	}
	m.lookupIndex++

	return []qbittorrent.Torrent{m.lookupSequence[idx]}, nil
}

func (m *mockQbitClient) GetFilesInformation(hash string) (*qbittorrent.TorrentFiles, error) {
	return m.filesByHash[hash], nil
}

func (m *mockQbitClient) AddTorrentFromMemory(buf []byte, options map[string]string) (*qbittorrent.TorrentAddResponse, error) {
	m.addBytes = append([]byte(nil), buf...)
	m.addOptions = make(map[string]string, len(options))
	maps.Copy(m.addOptions, options)
	return &qbittorrent.TorrentAddResponse{}, nil
}

func (m *mockQbitClient) GetCategories() (map[string]qbittorrent.Category, error) {
	if m.categoryErr != nil {
		return nil, m.categoryErr
	}
	return m.categories, nil
}

func (m *mockQbitClient) GetDefaultSavePath() (string, error) {
	if m.defaultSaveErr != nil {
		return "", m.defaultSaveErr
	}
	return m.defaultSave, nil
}

func (m *mockQbitClient) Recheck(hashes []string) error {
	m.recheckCalls = append(m.recheckCalls, append([]string(nil), hashes...))
	return nil
}

func (m *mockQbitClient) Resume(hashes []string) error {
	m.resumeCalls = append(m.resumeCalls, append([]string(nil), hashes...))
	return nil
}

func TestParseTorrentStandaloneImportsAndResumes(t *testing.T) {
	clientMap = xsync.NewMapOf[string, qbitClient]()
	entryMap = xsync.NewMapOf[string, *entryCache]()

	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	importDir := filepath.Join(tempDir, "import")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.MkdirAll(importDir, 0o755))

	releaseName := "Series.S01.1080p.WEB-DL.H.264-RlsGrp"
	torrentBytes, err := torrents.TorrentFromRls(releaseName, 2)
	require.NoError(t, err)

	infoHash, err := torrents.InfoHash(torrentBytes)
	require.NoError(t, err)

	ep1Name := "Series.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"
	ep2Name := "Series.S01E02.1080p.WEB-DL.H.264-RlsGrp.mkv"
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, ep1Name), []byte("0"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, ep2Name), []byte("0"), 0o644))

	ep1Files := qbittorrent.TorrentFiles{{Name: ep1Name, Size: 1}}
	ep2Files := qbittorrent.TorrentFiles{{Name: ep2Name, Size: 1}}

	mockClient := &mockQbitClient{
		allTorrents: []qbittorrent.Torrent{
			{Name: "Series.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: sourceDir},
			{Name: "Series.S01E02.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep2", SavePath: sourceDir},
		},
		filesByHash: map[string]*qbittorrent.TorrentFiles{
			"ep1": &ep1Files,
			"ep2": &ep2Files,
		},
		lookupSequence: []qbittorrent.Torrent{
			{Hash: infoHash, InfohashV1: infoHash, State: qbittorrent.TorrentStateMissingFiles},
			{Hash: infoHash, InfohashV1: infoHash, State: qbittorrent.TorrentStateCheckingDl},
			{Hash: infoHash, InfohashV1: infoHash, State: qbittorrent.TorrentStatePausedDl},
		},
		categories: map[string]qbittorrent.Category{
			"tv-hd": {Name: "tv-hd", SavePath: importDir},
		},
	}

	cfg := &config.AppConfig{
		Config: &domain.Config{
			Clients: map[string]*domain.Client{
				"default": {
					Qbit: domain.Qbit{
						Category:    "tv-hd",
						Tags:        []string{"seasonpackarr"},
						PausedOnAdd: true,
					},
				},
			},
		},
	}

	p := newProcessor(
		logger.New(&domain.Config{LogLevel: "ERROR", Version: "test"}),
		cfg,
		nil,
		nil,
	)
	p.req = &request{
		Name:       releaseName,
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		Client:     mockClient,
		ClientName: "default",
	}

	statusCode, err := p.parseTorrent()
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)

	require.NotEmpty(t, mockClient.addBytes)
	require.Equal(t, "true", mockClient.addOptions["paused"])
	require.Equal(t, "true", mockClient.addOptions["stopped"])
	require.Equal(t, "tv-hd", mockClient.addOptions["category"])
	require.Equal(t, "seasonpackarr", mockClient.addOptions["tags"])
	_, hasSavePath := mockClient.addOptions["savepath"]
	require.False(t, hasSavePath)
	_, hasContentLayout := mockClient.addOptions["contentLayout"]
	require.False(t, hasContentLayout)
	_, hasRootFolder := mockClient.addOptions["root_folder"]
	require.False(t, hasRootFolder)
	require.Len(t, mockClient.recheckCalls, 1)
	require.Equal(t, []string{infoHash}, mockClient.recheckCalls[0])
	require.Len(t, mockClient.resumeCalls, 1)
	require.Equal(t, []string{infoHash}, mockClient.resumeCalls[0])

	require.FileExists(t, filepath.Join(importDir, releaseName, ep1Name))
	require.FileExists(t, filepath.Join(importDir, releaseName, ep2Name))
}

func TestParseTorrentSkipsCrossSeedDuplicates(t *testing.T) {
	clientMap = xsync.NewMapOf[string, qbitClient]()
	entryMap = xsync.NewMapOf[string, *entryCache]()

	tempDir := t.TempDir()
	sourceDir1 := filepath.Join(tempDir, "source1")
	sourceDir2 := filepath.Join(tempDir, "source2")
	importDir := filepath.Join(tempDir, "import")
	require.NoError(t, os.MkdirAll(sourceDir1, 0o755))
	require.NoError(t, os.MkdirAll(sourceDir2, 0o755))
	require.NoError(t, os.MkdirAll(importDir, 0o755))

	releaseName := "Series.S01.1080p.WEB-DL.H.264-RlsGrp"
	torrentBytes, err := torrents.TorrentFromRls(releaseName, 2)
	require.NoError(t, err)

	infoHash, err := torrents.InfoHash(torrentBytes)
	require.NoError(t, err)

	ep1Name := "Series.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"
	ep2Name := "Series.S01E02.1080p.WEB-DL.H.264-RlsGrp.mkv"
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir1, ep1Name), []byte("0"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir1, ep2Name), []byte("0"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir2, ep1Name), []byte("0"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir2, ep2Name), []byte("0"), 0o644))

	ep1Files := qbittorrent.TorrentFiles{{Name: ep1Name, Size: 1}}
	ep2Files := qbittorrent.TorrentFiles{{Name: ep2Name, Size: 1}}

	// Two cross-seeded torrents for each episode at different paths
	mockClient := &mockQbitClient{
		allTorrents: []qbittorrent.Torrent{
			{Name: "Series.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1a", SavePath: sourceDir1},
			{Name: "Series.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1b", SavePath: sourceDir2},
			{Name: "Series.S01E02.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep2a", SavePath: sourceDir1},
			{Name: "Series.S01E02.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep2b", SavePath: sourceDir2},
		},
		filesByHash: map[string]*qbittorrent.TorrentFiles{
			"ep1a": &ep1Files,
			"ep1b": &ep1Files,
			"ep2a": &ep2Files,
			"ep2b": &ep2Files,
		},
		lookupSequence: []qbittorrent.Torrent{
			{Hash: infoHash, InfohashV1: infoHash, State: qbittorrent.TorrentStatePausedDl},
		},
		categories: map[string]qbittorrent.Category{
			"tv-hd": {Name: "tv-hd", SavePath: importDir},
		},
	}

	cfg := &config.AppConfig{
		Config: &domain.Config{
			Clients: map[string]*domain.Client{
				"default": {
					Qbit: domain.Qbit{
						Category: "tv-hd",
					},
				},
			},
		},
	}

	var logBuf bytes.Buffer
	zerolog.SetGlobalLevel(zerolog.DebugLevel)

	p := newProcessor(
		logger.New(&domain.Config{LogLevel: "DEBUG", Version: "test"}),
		cfg,
		nil,
		nil,
	)
	p.log = zerolog.New(&logBuf).With().Logger()
	p.req = &request{
		Name:       releaseName,
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		Client:     mockClient,
		ClientName: "default",
	}

	statusCode, err := p.parseTorrent()
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)

	// Each episode should have exactly one hardlink despite cross-seed duplicates
	require.FileExists(t, filepath.Join(importDir, releaseName, ep1Name))
	require.FileExists(t, filepath.Join(importDir, releaseName, ep2Name))

	// Verify duplicates were skipped, not attempted
	logOutput := logBuf.String()
	require.Equal(t, 2, strings.Count(logOutput, "skipping already linked target"))
}

func TestProcessSeasonPackIsGateOnly(t *testing.T) {
	clientMap = xsync.NewMapOf[string, qbitClient]()
	entryMap = xsync.NewMapOf[string, *entryCache]()

	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	importDir := filepath.Join(tempDir, "import")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.MkdirAll(importDir, 0o755))

	ep1Name := "Series.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, ep1Name), []byte("0"), 0o644))

	ep1Files := qbittorrent.TorrentFiles{{Name: ep1Name, Size: 1}}
	mockClient := &mockQbitClient{
		allTorrents: []qbittorrent.Torrent{
			{Name: "Series.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: sourceDir},
		},
		filesByHash: map[string]*qbittorrent.TorrentFiles{
			"ep1": &ep1Files,
		},
	}

	cfg := &config.AppConfig{
		Config: &domain.Config{
			Clients: map[string]*domain.Client{
				"default": {
					Qbit: domain.Qbit{
						Category:    "tv-hd",
						PausedOnAdd: true,
					},
				},
			},
		},
	}

	p := newProcessor(
		logger.New(&domain.Config{LogLevel: "ERROR", Version: "test"}),
		cfg,
		nil,
		nil,
	)
	p.req = &request{
		Name:       "Series.S01.1080p.WEB-DL.H.264-RlsGrp",
		Client:     mockClient,
		ClientName: "default",
	}

	statusCode, err := p.processSeasonPack()
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulMatch, statusCode)
	require.Empty(t, mockClient.addBytes)
	require.NoFileExists(t, filepath.Join(importDir, "Series.S01.1080p.WEB-DL.H.264-RlsGrp", ep1Name))
}

func TestBuildTorrentAddOptionsOmitsUnsetOverrides(t *testing.T) {
	options, statusCode, err := buildTorrentAddOptions(&domain.Client{
		Qbit: domain.Qbit{
			Category:    "tv-hd",
			PausedOnAdd: true,
		},
	})
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)

	prepared := options.Prepare()
	require.Equal(t, "tv-hd", prepared["category"])
	require.Equal(t, "true", prepared["paused"])
	require.Equal(t, "true", prepared["stopped"])
	_, hasSavePath := prepared["savepath"]
	require.False(t, hasSavePath)
	_, hasDownloadPath := prepared["downloadPath"]
	require.False(t, hasDownloadPath)
	_, hasContentLayout := prepared["contentLayout"]
	require.False(t, hasContentLayout)
	_, hasRootFolder := prepared["root_folder"]
	require.False(t, hasRootFolder)
}

func TestBuildTorrentAddOptionsUsesExplicitLayout(t *testing.T) {
	options, statusCode, err := buildTorrentAddOptions(&domain.Client{
		Qbit: domain.Qbit{
			Category:      "tv-hd",
			PausedOnAdd:   true,
			ContentLayout: "subfolder",
		},
	})
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)
	require.Equal(t, string(qbittorrent.ContentLayoutSubfolderCreate), options.Prepare()["contentLayout"])
}

func TestBuildTorrentAddOptionsSetsDownloadPath(t *testing.T) {
	options, statusCode, err := buildTorrentAddOptions(&domain.Client{
		Qbit: domain.Qbit{
			Category:     "tv-hd",
			DownloadPath: "/data/incomplete",
		},
	})
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)

	prepared := options.Prepare()
	require.Equal(t, "/data/incomplete", prepared["downloadPath"])
	require.Equal(t, "true", prepared["useDownloadPath"])
}

func TestBuildTorrentAddOptionsRejectsInvalidLayout(t *testing.T) {
	_, statusCode, err := buildTorrentAddOptions(&domain.Client{
		Qbit: domain.Qbit{
			Category:      "tv-hd",
			ContentLayout: "bad-layout",
		},
	})
	require.Error(t, err)
	require.Equal(t, domain.StatusQbitConfigError, statusCode)
}

func TestResolveImportRootCategoryOnly(t *testing.T) {
	tempDir := t.TempDir()
	importDir := filepath.Join(tempDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0o755))

	tests := []struct {
		name        string
		categories  map[string]qbittorrent.Category
		defaultSave string
		wantRoot    string
		wantStatus  domain.StatusCode
		wantErr     bool
	}{
		{
			name: "category save path matches",
			categories: map[string]qbittorrent.Category{
				"tv-hd": {Name: "tv-hd", SavePath: importDir},
			},
			wantRoot:   importDir,
			wantStatus: domain.StatusSuccessfulHardlink,
		},
		{
			name: "default save path fallback matches",
			categories: map[string]qbittorrent.Category{
				"tv-hd": {Name: "tv-hd", SavePath: ""},
			},
			defaultSave: importDir,
			wantRoot:    importDir,
			wantStatus:  domain.StatusSuccessfulHardlink,
		},
		{
			name: "category save path is used as destination",
			categories: map[string]qbittorrent.Category{
				"tv-hd": {Name: "tv-hd", SavePath: filepath.Join(tempDir, "other")},
			},
			wantRoot:   filepath.Join(tempDir, "other"),
			wantStatus: domain.StatusSuccessfulHardlink,
		},
		{
			name: "relative category save path resolved against default",
			categories: map[string]qbittorrent.Category{
				"tv-hd": {Name: "tv-hd", SavePath: "import"},
			},
			defaultSave: tempDir,
			wantRoot:    importDir,
			wantStatus:  domain.StatusSuccessfulHardlink,
		},
		{
			name: "relative category save path resolves against default",
			categories: map[string]qbittorrent.Category{
				"tv-hd": {Name: "tv-hd", SavePath: "other"},
			},
			defaultSave: tempDir,
			wantRoot:    filepath.Join(tempDir, "other"),
			wantStatus:  domain.StatusSuccessfulHardlink,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &processor{
				req: &request{
					Client: &mockQbitClient{
						categories:  tt.categories,
						defaultSave: tt.defaultSave,
					},
				},
			}

			root, statusCode, err := p.resolveImportRoot(&domain.Client{
				Qbit: domain.Qbit{
					Category: "tv-hd",
				},
			})
			require.Equal(t, tt.wantStatus, statusCode)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, normalizePath(tt.wantRoot), root)
		})
	}
}

func TestResolveImportRootExplicitSavePath(t *testing.T) {
	tempDir := t.TempDir()
	importDir := filepath.Join(tempDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0o755))

	p := &processor{req: &request{Client: &mockQbitClient{}}}

	root, statusCode, err := p.resolveImportRoot(&domain.Client{
		Qbit: domain.Qbit{
			SavePath: importDir,
		},
	})
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)
	require.Equal(t, normalizePath(importDir), root)
}

func TestResolveImportRootRejectsQbitLookupFailures(t *testing.T) {
	t.Run("category read error", func(t *testing.T) {
		p := &processor{req: &request{Client: &mockQbitClient{
			categoryErr: errors.New("category boom"),
		}}}

		_, statusCode, err := p.resolveImportRoot(&domain.Client{
			Qbit: domain.Qbit{
				Category: "tv-hd",
			},
		})
		require.Error(t, err)
		require.Equal(t, domain.StatusQbitConfigError, statusCode)
	})

	t.Run("missing category", func(t *testing.T) {
		p := &processor{req: &request{Client: &mockQbitClient{
			categories: map[string]qbittorrent.Category{},
		}}}

		_, statusCode, err := p.resolveImportRoot(&domain.Client{
			Qbit: domain.Qbit{
				Category: "tv-hd",
			},
		})
		require.Error(t, err)
		require.Equal(t, domain.StatusQbitConfigError, statusCode)
	})

	t.Run("default save path error", func(t *testing.T) {
		p := &processor{req: &request{Client: &mockQbitClient{
			categories: map[string]qbittorrent.Category{
				"tv-hd": {Name: "tv-hd", SavePath: ""},
			},
			defaultSaveErr: errors.New("default boom"),
		}}}

		_, statusCode, err := p.resolveImportRoot(&domain.Client{
			Qbit: domain.Qbit{
				Category: "tv-hd",
			},
		})
		require.Error(t, err)
		require.Equal(t, domain.StatusQbitConfigError, statusCode)
	})

	t.Run("empty resolved destination", func(t *testing.T) {
		p := &processor{req: &request{Client: &mockQbitClient{
			categories: map[string]qbittorrent.Category{
				"tv-hd": {Name: "tv-hd", SavePath: ""},
			},
		}}}

		_, statusCode, err := p.resolveImportRoot(&domain.Client{
			Qbit: domain.Qbit{
				Category: "tv-hd",
			},
		})
		require.Error(t, err)
		require.Equal(t, domain.StatusQbitConfigError, statusCode)
	})
}

func TestParseTorrentUsesSavePathAsHardlinkRoot(t *testing.T) {
	clientMap = xsync.NewMapOf[string, qbitClient]()
	entryMap = xsync.NewMapOf[string, *entryCache]()

	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	saveDir := filepath.Join(tempDir, "save")
	downloadDir := filepath.Join(tempDir, "download")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.MkdirAll(saveDir, 0o755))
	require.NoError(t, os.MkdirAll(downloadDir, 0o755))

	releaseName := "Series.S01.1080p.WEB-DL.H.264-RlsGrp"
	torrentBytes, err := torrents.TorrentFromRls(releaseName, 1)
	require.NoError(t, err)

	infoHash, err := torrents.InfoHash(torrentBytes)
	require.NoError(t, err)

	epName := "Series.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, epName), []byte("0"), 0o644))

	epFiles := qbittorrent.TorrentFiles{{Name: epName, Size: 1}}
	mockClient := &mockQbitClient{
		allTorrents: []qbittorrent.Torrent{
			{Name: "Series.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: sourceDir},
		},
		filesByHash: map[string]*qbittorrent.TorrentFiles{
			"ep1": &epFiles,
		},
		lookupSequence: []qbittorrent.Torrent{
			{Hash: infoHash, InfohashV1: infoHash, State: qbittorrent.TorrentStatePausedDl},
		},
		categories: map[string]qbittorrent.Category{
			"tv-hd": {Name: "tv-hd", SavePath: filepath.Join(tempDir, "category")},
		},
	}

	cfg := &config.AppConfig{
		Config: &domain.Config{
			Clients: map[string]*domain.Client{
				"default": {
					Qbit: domain.Qbit{
						Category:     "tv-hd",
						SavePath:     saveDir,
						DownloadPath: downloadDir,
						PausedOnAdd:  true,
					},
				},
			},
		},
	}

	p := newProcessor(
		logger.New(&domain.Config{LogLevel: "ERROR", Version: "test"}),
		cfg,
		nil,
		nil,
	)
	p.req = &request{
		Name:       releaseName,
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		Client:     mockClient,
		ClientName: "default",
	}

	statusCode, err := p.parseTorrent()
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)

	require.FileExists(t, filepath.Join(saveDir, releaseName, epName))
	require.NoFileExists(t, filepath.Join(downloadDir, releaseName, epName))
	require.Equal(t, saveDir, mockClient.addOptions["savepath"])
	require.Equal(t, downloadDir, mockClient.addOptions["downloadPath"])
}

func TestResolveImportRootDoesNotUseDownloadPath(t *testing.T) {
	tempDir := t.TempDir()
	saveDir := filepath.Join(tempDir, "save")
	downloadDir := filepath.Join(tempDir, "download")
	require.NoError(t, os.MkdirAll(saveDir, 0o755))
	require.NoError(t, os.MkdirAll(downloadDir, 0o755))

	p := &processor{req: &request{Client: &mockQbitClient{}}}

	root, statusCode, err := p.resolveImportRoot(&domain.Client{
		Qbit: domain.Qbit{
			SavePath:     saveDir,
			DownloadPath: downloadDir,
		},
	})
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)
	require.Equal(t, normalizePath(saveDir), root)
}
