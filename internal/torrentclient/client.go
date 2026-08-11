// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
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
// hardlinks. LegacyHash is the SHA-1 client identifier. V2Hash is the BEP 52
// SHA-256 identity. HasV1 distinguishes hybrid torrents from pure v2 torrents.
type ImportRequest struct {
	TorrentBytes []byte
	SavePath     string
	LegacyHash   string
	V2Hash       string
	HasV1        bool
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

// ImportStage identifies which step of Import failed without exposing a
// client-specific error type to the processor.
type ImportStage int

const (
	ImportStageConfig      ImportStage = iota // invalid local policy or invariant
	ImportStageRequest                        // supplied torrent unsupported by the selected client
	ImportStageDestination                    // reading destination state from the client
	ImportStageAdd                            // adding the torrent to the client
	ImportStageFind                           // locating the added torrent
	ImportStageRecheck                        // rechecking / verifying local data
	ImportStageResume                         // resuming the torrent
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

// ImportFailed converts an error returned by ImportDestination or Import to a
// processing outcome. Invalid local state is internal, unsupported input is a
// request failure, and client operations are dependency failures. Untagged
// errors use the add-torrent fallback.
func ImportFailed(err error) domain.Outcome {
	if ie, ok := stderrors.AsType[*ImportError](err); ok {
		switch ie.Stage {
		case ImportStageConfig:
			return domain.FailedBecause(domain.ReasonImportConfigInvalid, domain.FaultInternal, err)
		case ImportStageRequest:
			return domain.FailedBecause(domain.ReasonTorrentUnsupported, domain.FaultRequest, err)
		case ImportStageDestination:
			return domain.FailedBecause(domain.ReasonImportDestinationFailed, domain.FaultDependency, err)
		case ImportStageAdd:
			return domain.FailedBecause(domain.ReasonTorrentAddFailed, domain.FaultDependency, err)
		case ImportStageFind:
			return domain.FailedBecause(domain.ReasonImportedTorrentNotFound, domain.FaultDependency, err)
		case ImportStageRecheck:
			return domain.FailedBecause(domain.ReasonTorrentRecheckFailed, domain.FaultDependency, err)
		case ImportStageResume:
			return domain.FailedBecause(domain.ReasonTorrentResumeFailed, domain.FaultDependency, err)
		}
	}
	return domain.FailedBecause(domain.ReasonTorrentAddFailed, domain.FaultDependency, err)
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
// conditional recheck on qBittorrent, forced verify on Transmission, Deluge's
// normal initial check) and starts it without leaving the import paused.
// Import failures are returned as *ImportError.
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
	case "deluge", "deluge-v1", "deluge-v2":
		return newDelugeClient(client)
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
