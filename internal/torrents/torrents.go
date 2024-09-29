// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrents

import (
	"bytes"
	"cmp"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/anacrolix/torrent/metainfo"
)

type Episode struct {
	Path string
	Size int64
}

func ParseInfoFromTorrentBytes(torrentBytes []byte) (metainfo.Info, error) {
	metaInfo, err := metainfo.Load(bytes.NewReader(torrentBytes))
	if err != nil {
		return metainfo.Info{}, err
	}

	return metaInfo.UnmarshalInfo()
}

func GetEpisodesFromTorrentInfo(info metainfo.Info) ([]Episode, error) {
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
