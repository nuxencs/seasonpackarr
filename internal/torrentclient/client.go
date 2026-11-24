// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"context"
	"errors"
	"net"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/errtrace"
	"github.com/nuxencs/seasonpackarr/internal/placement"
)

// Torrent is the neutral view of a torrent shared across client implementations.
// It has no save path: the candidate check needs only the hash and the name, and
// GetFiles returns the save path together with the files.
type Torrent struct {
	Hash string
	Name string
}

// File is the neutral view of a single file within a torrent.
//
// Name MUST be the file path relative to FileResult.SavePath, including the
// torrent's root folder (e.g. "Show.S01/Show.S01E01.mkv" for a multi-file
// torrent). This is load-bearing: the processor hardlinks
// filepath.Join(FileResult.SavePath, File.Name), so returning a bare basename
// here would silently break hardlinking.
type File struct {
	Name string
	Size int64
}

// FileResult is one ordered response to a hash supplied to GetFiles. Err is
// scoped to that hash, so callers can keep successful results from a partial
// read.
//
// SavePath MUST be the absolute on-disk directory of the torrent content, read
// in the same GetFiles call as Files. A client can move a torrent between two
// reads, so a save path from an earlier read can give a stale source path.
type FileResult struct {
	Hash     string
	SavePath string
	Files    []File
	Err      error
}

// ImportRequest carries a parsed season pack to be (re-)imported into the
// client on /api/import. The processor has already hardlinked the matched local
// episodes into SavePath before Import is called.
//
// SavePath MUST equal the root of the destination that ImportDestination gave
// for the same client, so the client adds the torrent pointing at the directory
// that already contains the hardlinks. LegacyHash is the SHA-1 client identifier. V2Hash is the BEP 52
// SHA-256 identity. HasV1 distinguishes hybrid torrents from pure v2 torrents.
// DataComplete reports that every torrent file already exists at its import
// target with the expected size, so a client may skip its full hash check.
type ImportRequest struct {
	TorrentBytes []byte
	SavePath     string
	LegacyHash   string
	V2Hash       string
	HasV1        bool
	DataComplete bool
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

func (s ImportStage) String() string {
	switch s {
	case ImportStageConfig:
		return "config"
	case ImportStageAdd:
		return "add"
	case ImportStageFind:
		return "find"
	case ImportStageRecheck:
		return "recheck"
	case ImportStageResume:
		return "resume"
	default:
		return "unknown"
	}
}

// ImportStageReport records one successful or failed client-import stage.
type ImportStageReport struct {
	Stage    ImportStage
	Duration time.Duration
}

// ImportReport describes the client work attempted before Import returned.
// Stages stay in execution order.
type ImportReport struct {
	Stages []ImportStageReport
}

func (r *ImportReport) record(stage ImportStage, started time.Time) {
	r.Stages = append(r.Stages, ImportStageReport{Stage: stage, Duration: time.Since(started)})
}

// ImportError tags an import failure with the stage it happened in.
type ImportError struct {
	Stage ImportStage
	Err   error
}

func (e *ImportError) Error() string { return e.Err.Error() }
func (e *ImportError) Unwrap() error { return e.Err }

func importErr(stage ImportStage, err error) *ImportError {
	if stage != ImportStageConfig {
		err = errtrace.WithStack(err)
	}
	return &ImportError{Stage: stage, Err: err}
}

// ImportStatusCode maps an error returned by ImportDestination or Import to the
// domain.StatusCode the caller should report. Errors that aren't a stage-tagged
// *ImportError fall back to StatusAddTorrentError.
func ImportStatusCode(err error) domain.StatusCode {
	if ie, ok := errors.AsType[*ImportError](err); ok {
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
// Read contract: each result of GetFiles must satisfy
// filepath.Join(FileResult.SavePath, File.Name) == the real absolute on-disk
// path of the file, so it can be used directly as a hardlink source. The save
// path and the files of a result come from one GetFiles call. The hash
// returned by GetTorrents must be accepted as-is by GetFiles.
//
// Import contract: ImportDestination resolves the absolute directory and file
// layout the season pack must use. Import adds the pack so the client accounts
// for its already-present hardlinked data and then starts it by itself. Import
// must not wait for a client hash check: a large pack can take longer than the
// caller's request timeout, so the client owns the check after the add.
// Import returns neutral stage timings in execution order. Import failures are
// returned as *ImportError together with the attempted stage timings.
//
// Close contract: an adapter can also implement io.Closer. The inventory
// clean-up closes an adapter that it drops, but a request can still hold that
// adapter, so Close must leave it usable. The Deluge adapter connects again on
// its next call.
type TorrentClient interface {
	GetTorrents(ctx context.Context) ([]Torrent, error)
	// GetFiles returns exactly one result per input hash in input order. Each
	// adapter owns its safe batching or concurrency strategy.
	GetFiles(ctx context.Context, hashes []string) []FileResult
	ImportDestination(ctx context.Context) (placement.Destination, error)
	Import(ctx context.Context, req ImportRequest) (ImportReport, error)
}

func fileResultsWithError(hashes []string, err error) []FileResult {
	results := make([]FileResult, len(hashes))
	for index, hash := range hashes {
		results[index] = FileResult{Hash: hash, Err: err}
	}
	return results
}

// pollUntil invokes cond every interval until it reports done, cond returns an
// error, or timeout elapses. It is shared by the client adapters to wait for a
// torrent to appear or for a (re)check to settle.
func pollUntil(ctx context.Context, interval time.Duration, cond func() (bool, error)) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		done, err := cond()
		if err != nil {
			return err
		}
		if done {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// New validates client and connects to it. No adapter exists for an invalid config.
func New(ctx context.Context, client *domain.Client) (TorrentClient, error) {
	entry, err := validate(*client)
	if err != nil {
		return nil, err
	}
	return entry.newAdapter(ctx, client)
}

// Endpoint identifies the client service that a client config connects to.
// Configs with the same endpoint share one service, so an import through one
// of them changes what the others see. Its fields are unexported, so a caller
// can only compare it and use it as a map key.
type Endpoint struct {
	typeName string
	scheme   string
	host     string
	port     int
	path     string
}

// defaultPorts are the ports that an HTTP address with no port connects to.
var defaultPorts = map[string]int{"http": 80, "https": 443}

// EndpointOf returns the endpoint of client: the canonical client type and the
// address that the adapter connects to, in each spelling of the config. It does
// no DNS lookup, so a host name and its IP address are two endpoints. A config
// whose address does not build gets the endpoint of its trimmed host and port.
func EndpointOf(client domain.Client) Endpoint {
	raw := Endpoint{typeName: client.Type, host: strings.TrimSpace(client.Host), port: client.Port}
	entry, ok := lookupClientType(client.Type)
	if !ok {
		return raw
	}
	raw.typeName = entry.name
	address, err := entry.address(client)
	if err != nil {
		return raw
	}

	port := defaultPorts[address.Scheme]
	if address.Port() != "" {
		// url.Parse accepts only digits as a port, so only a port too large for an int fails here.
		port, _ = strconv.Atoi(address.Port())
	}
	// go-qbittorrent joins the API path with url.JoinPath, which cleans the path,
	// so "/proxy/abc/" and "/proxy/abc" reach the same service. The Transmission
	// path is fixed, and a Deluge address has no path.
	cleanPath := strings.TrimSuffix(path.Clean("/"+address.EscapedPath()), "/")
	return Endpoint{
		typeName: entry.name,
		scheme:   address.Scheme,
		host:     strings.ToLower(address.Hostname()),
		port:     port,
		path:     cleanPath,
	}
}

// normalizePath cleans and normalizes a client-reported destination path so it
// can be used consistently as a hardlink root.
func normalizePath(path string) string {
	return filepath.Clean(filepath.FromSlash(strings.TrimSpace(path)))
}

// httpAddress is the address build of the client types that connect over HTTP.
// A host without a scheme gets http, and the port setting wins over a port in host.
func httpAddress(client domain.Client) (*url.URL, error) {
	if strings.TrimSpace(client.Host) == "" {
		return nil, errors.New("host is required")
	}

	host := client.Host
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}

	parsedURL, err := url.Parse(host)
	if err != nil {
		// Each parse error quotes a part of the host, and a path or user info in it can be a secret.
		return nil, errors.New("host does not parse as a URL")
	}
	if parsedURL.Hostname() == "" {
		return nil, errors.New("host has no host name")
	}

	if client.Port != 0 {
		parsedURL.Host = net.JoinHostPort(parsedURL.Hostname(), strconv.Itoa(client.Port))
	}

	return parsedURL, nil
}
