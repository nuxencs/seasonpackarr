// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrents

import (
	"bytes"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/anacrolix/torrent/metainfo"
)

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

func GetEpisodesFromTorrentInfo(info metainfo.Info) ([]string, error) {
	var fileNames []string

	if !info.IsDir() {
		return []string{}, fmt.Errorf("not a directory")
	}

	for _, file := range info.Files {
		if filepath.Ext(file.BestPath()[0]) == ".mkv" {
			fileNames = append(fileNames, file.BestPath()...)
		}
	}

	if len(fileNames) == 0 {
		return []string{}, fmt.Errorf("no .mkv files found")
	}

	slices.Sort(fileNames)
	return fileNames, nil
}
