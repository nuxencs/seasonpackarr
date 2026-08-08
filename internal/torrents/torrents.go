// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrents

import (
	"bytes"
	"cmp"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/autobrr/go-torrent/metainfo"
)

type Episode struct {
	Path string
	Size int64
}

func Info(torrent []byte) (metainfo.Info, error) {
	metaInfo, err := metainfo.Load(bytes.NewReader(torrent))
	if err != nil {
		return metainfo.Info{}, err
	}

	return metaInfo.UnmarshalInfo()
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

func Episodes(info metainfo.Info) ([]Episode, error) {
	if !info.IsDir() {
		return []Episode{}, fmt.Errorf("not a directory")
	}

	files := info.UpvertedFiles()
	episodes := make([]Episode, 0, len(files))

	for _, file := range files {
		path := file.DisplayPath(&info)

		if filepath.Ext(path) != ".mkv" {
			continue
		}

		episodes = append(episodes, Episode{
			Path: path,
			Size: file.Length,
		})
	}

	if len(episodes) == 0 {
		return []Episode{}, fmt.Errorf("no .mkv files found")
	}

	if len(episodes) > 1 {
		slices.SortStableFunc(episodes, func(a, b Episode) int {
			return cmp.Compare(a.Path, b.Path)
		})
	}

	return episodes, nil
}
