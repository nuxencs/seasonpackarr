// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	stderrors "errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/pkg/errors"
)

// Torrent is the neutral view of a torrent shared across client implementations.
//
// SavePath MUST be the absolute on-disk directory the torrent's content was
// downloaded into. Together with File.Name it forms the hardlink source path
// (see TorrentClient).
type Torrent struct {
	Hash     string
	Name     string
	SavePath string
}

// File is the neutral view of a single file within a torrent.
//
// Name MUST be the file path relative to the owning Torrent.SavePath, including
// the torrent's root folder (e.g. "Show.S01/Show.S01E01.mkv" for a multi-file
// torrent). This is load-bearing: the processor hardlinks
// filepath.Join(Torrent.SavePath, File.Name), so returning a bare basename here
// would silently break hardlinking.
type File struct {
	Name string
	Size int64
}

// ImportRequest carries a parsed season pack to be (re-)imported into the
// client on /api/parse. The processor has already hardlinked the matched local
// episodes into SavePath before Import is called.
//
// SavePath MUST equal ImportDestination.SavePath for the same client, so the
// client adds the torrent pointing at the directory that already contains the
// hardlinks. Hash is the hex v1 info hash (see torrents.InfoHash) used to look
// the torrent up after it is added.
type ImportRequest struct {
	TorrentBytes []byte
	Hash         string
	SavePath     string
}

// ImportDestination describes both the client save path and the on-disk file
// layout expected beneath it. Callers do not need to know which client-specific
// option selected the layout.
type ImportDestination struct {
	root               string
	includeTorrentRoot bool
}

func NewRootedImportDestination(root string) ImportDestination {
	return ImportDestination{root: root, includeTorrentRoot: true}
}

func NewFlatImportDestination(root string) ImportDestination {
	return ImportDestination{root: root}
}

func (d ImportDestination) SavePath() string {
	return d.root
}

func (d ImportDestination) TargetPath(torrentName, torrentFilePath string) string {
	if d.includeTorrentRoot {
		return filepath.Join(d.root, torrentName, torrentFilePath)
	}

	return filepath.Join(d.root, torrentFilePath)
}

// ImportStage identifies which step of Import failed so the caller can map it
// to a domain.StatusCode without importing any client-specific type.
type ImportStage int

const (
	ImportStageConfig  ImportStage = iota // resolving destination / building options
	ImportStageAdd                        // adding the torrent to the client
	ImportStageFind                       // locating the added torrent
	ImportStageRecheck                    // rechecking / verifying local data
	ImportStageResume                     // resuming the torrent
)

// ImportError tags an import failure with the stage it happened in.
type ImportError struct {
	Stage ImportStage
	Err   error
}

func (e *ImportError) Error() string { return e.Err.Error() }
func (e *ImportError) Unwrap() error { return e.Err }

func importErr(stage ImportStage, err error) *ImportError {
	return &ImportError{Stage: stage, Err: err}
}

// ImportStatusCode maps an error returned by ImportDestination or Import to the
// domain.StatusCode the caller should report. Errors that aren't a stage-tagged
// *ImportError fall back to StatusAddTorrentError.
func ImportStatusCode(err error) domain.StatusCode {
	if ie, ok := stderrors.AsType[*ImportError](err); ok {
		switch ie.Stage {
		case ImportStageConfig:
			return domain.StatusImportConfigError
		case ImportStageAdd:
			return domain.StatusAddTorrentError
		case ImportStageFind:
			return domain.StatusFindTorrentError
		case ImportStageRecheck:
			return domain.StatusRecheckTorrentError
		case ImportStageResume:
			return domain.StatusResumeTorrentError
		}
	}
	return domain.StatusAddTorrentError
}

// TorrentClient is the surface the processor needs from a torrent client.
//
// Read contract: the values returned by GetTorrents/GetFiles must satisfy
// filepath.Join(Torrent.SavePath, File.Name) == the real absolute on-disk path
// of the file, so it can be used directly as a hardlink source. The hash
// returned by GetTorrents must be accepted as-is by GetFiles.
//
// Import contract: ImportDestination resolves the absolute directory and file
// layout the season pack must use. Import adds the pack,
// ensures its already-present hardlinked data is accounted for (skip-check +
// conditional recheck on qBittorrent, forced verify on transmission) and starts
// it once the data has been (re)checked. Import failures are returned as
// *ImportError.
type TorrentClient interface {
	GetTorrents() ([]Torrent, error)
	GetFiles(hash string) ([]File, error)
	ImportDestination() (ImportDestination, error)
	Import(req ImportRequest) error
}

// pollUntil invokes cond every interval until it reports done, cond returns an
// error, or timeout elapses. It is shared by the client adapters to wait for a
// torrent to appear or for a (re)check to settle.
func pollUntil(interval, timeout time.Duration, cond func() (bool, error)) error {
	deadline := time.Now().Add(timeout)
	for {
		done, err := cond()
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		if !time.Now().Before(deadline) {
			return errors.New("timed out")
		}
		time.Sleep(interval)
	}
}

func New(client *domain.Client) (TorrentClient, error) {
	switch client.Type {
	case "", "qbittorrent":
		return newQbitClient(client)
	case "transmission":
		return newTransmissionClient(client)
	default:
		return nil, fmt.Errorf("unknown client type: %s", client.Type)
	}
}

// normalizePath cleans and normalizes a client-reported destination path so it
// can be used consistently as a hardlink root.
func normalizePath(path string) string {
	return filepath.Clean(filepath.FromSlash(strings.TrimSpace(path)))
}

func buildHost(client *domain.Client) (string, error) {
	if len(client.Host) == 0 {
		return "", errors.New("host is required")
	}

	host := client.Host
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}

	parsedURL, err := url.Parse(host)
	if err != nil {
		return "", errors.Wrap(err, "failed to parse host")
	}

	if client.Port != 0 {
		parsedURL.Host = net.JoinHostPort(parsedURL.Hostname(), strconv.Itoa(client.Port))
	}

	return parsedURL.String(), nil
}
