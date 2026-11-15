// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrents

import (
	"bytes"
	"cmp"
	"fmt"
	"iter"
	"path/filepath"
	"slices"
	"strings"

	"github.com/autobrr/go-torrent/metainfo"
)

type Episode struct {
	Path string
	Size int64
}

// File is one file of a torrent.
type File struct {
	// Path uses "/" and is relative to the pack folder. For a single-file
	// torrent it is the torrent name.
	Path string
	Size int64
	// Padding marks a BEP 47 padding file, which a client never writes to disk.
	Padding bool
}

// Info is the validated metadata of one torrent. Only ParseInfo makes a value
// with content, so each name and file path that an Info gives passed the path
// check. The zero value is a torrent with no name and no files.
type Info struct {
	name     string
	dir      bool
	files    []File
	length   int64
	episodes []Episode
}

// ParseInfo parses the torrent metadata. It rejects a torrent whose name or
// file paths can leave the pack folder, because both become hardlink targets
// below the import root.
func ParseInfo(torrent []byte) (Info, error) {
	metaInfo, err := metainfo.Load(bytes.NewReader(torrent))
	if err != nil {
		return Info{}, err
	}

	raw, err := metaInfo.UnmarshalInfo()
	if err != nil {
		return Info{}, err
	}

	name := raw.BestName()
	if name == "." || !filepath.IsLocal(name) || filepath.Base(name) != name {
		return Info{}, fmt.Errorf("torrent name %q is not a single folder or file name", name)
	}

	rawFiles := raw.UpvertedFiles()
	info := Info{name: name, dir: raw.IsDir(), files: make([]File, 0, len(rawFiles))}
	for _, file := range rawFiles {
		// The one file of a single-file torrent has the name as its path, which passed the check above.
		path := file.DisplayPath(&raw)
		if !filepath.IsLocal(filepath.FromSlash(path)) {
			return Info{}, fmt.Errorf("torrent file path %q leaves the pack folder", path)
		}
		info.files = append(info.files, File{
			Path:    path,
			Size:    file.Length,
			Padding: strings.Contains(file.Attr, "p"),
		})
		info.length += file.Length
	}
	if info.dir {
		info.episodes = listEpisodes(info.files)
	}

	return info, nil
}

// Name returns the torrent name: the name of the pack folder of a directory
// torrent, or the file name of a single-file torrent.
func (info Info) Name() string {
	return info.name
}

// IsDir reports whether the torrent is a directory torrent, which holds its
// files in a pack folder.
func (info Info) IsDir() bool {
	return info.dir
}

// Files yields each file of the torrent in torrent order.
func (info Info) Files() iter.Seq[File] {
	return slices.Values(info.files)
}

// TotalLength returns the sum of all file sizes, padding files included.
func (info Info) TotalLength() int64 {
	return info.length
}

// Episodes returns the video files of a directory torrent, sorted by path. A
// single-file torrent and a torrent with no video file give an error.
func (info Info) Episodes() ([]Episode, error) {
	if !info.dir {
		return []Episode{}, fmt.Errorf("not a directory")
	}
	if len(info.episodes) == 0 {
		return []Episode{}, fmt.Errorf("no supported video files found")
	}

	// A copy, so that a caller cannot change the validated paths.
	return slices.Clone(info.episodes), nil
}

func listEpisodes(files []File) []Episode {
	var episodes []Episode

	for _, file := range files {
		switch strings.ToLower(filepath.Ext(file.Path)) {
		case ".mkv", ".mp4":
		default:
			continue
		}

		episodes = append(episodes, Episode{
			Path: file.Path,
			Size: file.Size,
		})
	}

	slices.SortStableFunc(episodes, func(a, b Episode) int {
		return cmp.Compare(a.Path, b.Path)
	})

	return episodes
}

type Hashes struct {
	Legacy string
	V2     string
	HasV1  bool
}

// InfoHashes returns the client identifiers derived from the raw info bytes.
// Legacy is the SHA-1 identifier used by v1 torrents and Transmission's
// hashString. V2 is the BEP 52 SHA-256 identity when the torrent has v2 data.
func InfoHashes(torrent []byte) (Hashes, error) {
	metaInfo, err := metainfo.Load(bytes.NewReader(torrent))
	if err != nil {
		return Hashes{}, err
	}

	info, err := metaInfo.UnmarshalInfo()
	if err != nil {
		return Hashes{}, err
	}

	hashes := Hashes{
		Legacy: metaInfo.HashInfoBytes().HexString(),
		HasV1:  info.HasV1(),
	}
	if info.HasV2() {
		v2Hash := metainfo.HashV2Bytes(metaInfo.InfoBytes)
		hashes.V2 = v2Hash.HexString()
	}

	return hashes, nil
}
