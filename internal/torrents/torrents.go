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
	Name string
	Size int64
}

func ParseTorrentInfoFromTorrentBytes(torrentBytes []byte) (metainfo.Info, error) {
	r := bytes.NewReader(torrentBytes)

	metaInfo, err := metainfo.Load(r)
	if err != nil {
		return metainfo.Info{}, err
	}

	info, err := metaInfo.UnmarshalInfo()
	if err != nil {
		return metainfo.Info{}, err
	}

	return info, nil
}

func GetEpisodesFromTorrentInfo(info metainfo.Info) ([]Episode, error) {
	var episodes []Episode

	if !info.IsDir() {
		return []Episode{}, fmt.Errorf("not a directory")
	}

	for _, file := range info.Files {
		for _, path := range file.BestPath() {
			if filepath.Ext(path) != ".mkv" {
				continue
			}

			episodes = append(episodes, Episode{
				Name: path,
				Size: file.Length,
			})
			break
		}
	}

	if len(episodes) == 0 {
		return []Episode{}, fmt.Errorf("no .mkv files found")
	}

	slices.SortStableFunc(episodes, func(a, b Episode) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return episodes, nil
}
