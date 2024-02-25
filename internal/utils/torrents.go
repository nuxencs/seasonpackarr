// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package utils

import (
	"bytes"
	"encoding/base64"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"seasonpackarr/pkg/errors"

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

func GetEpisodeNamesFromTorrentInfo(info metainfo.Info) ([]string, error) {
	var fileNames []string

	if info.IsDir() {
		for _, file := range info.Files {
			if filepath.Ext(file.BestPath()[0]) == ".mkv" {
				fileNames = append(fileNames, file.BestPath()...)
			}
		}
		if len(fileNames) == 0 {
			return []string{}, errors.New("no .mkv files found")
		}
		slices.Sort(fileNames)
		return fileNames, nil
	}

	return []string{}, errors.New("not a directory")
}

func DecodeTorrentDataRawBytes(torrentBytes []byte) ([]byte, error) {
	var tb []byte
	var err error

	if tb, err = base64.StdEncoding.DecodeString(strings.Trim(strings.TrimSpace(string(torrentBytes)), `"`)); err == nil {
		return tb, nil
	} else {
		ts := strings.Trim(strings.TrimSpace(string(torrentBytes)), `\"[`)
		b := make([]byte, 0, len(ts)/3)

		for {
			r, valid, z := atoi(ts)
			if !valid {
				break
			}

			b = append(b, byte(r))
			ts = z
		}

		if len(b) != 0 {
			return b, nil
		}
	}

	return []byte{}, err
}

func atoi(buf string) (ret int, valid bool, pos string) {
	if len(buf) == 0 {
		return ret, false, buf
	}

	i := 0
	for ; unicode.IsSpace(rune(buf[i])); i++ {
	}

	r := buf[i]
	if r == '-' || r == '+' {
		i++
	}

	for ; i != len(buf); i++ {
		d := int(buf[i] - '0')
		if d < 0 || d > 9 {
			break
		}

		valid = true
		ret *= 10
		ret += d
	}

	if r == '-' {
		ret *= -1
	}

	return ret, valid, buf[i:]
}
