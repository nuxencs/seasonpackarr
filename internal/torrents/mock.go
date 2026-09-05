// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrents

import (
	"bytes"
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	regexp "github.com/dlclark/regexp2"
)

var seasonRegex = regexp.MustCompile(`\bS\d+\b(?!E\d+\b)`, regexp.IgnoreCase)

func mockEpisodes(dir string, numEpisodes int) error {
	match, err := seasonRegex.FindStringMatch(filepath.Base(dir))
	if err != nil {
		return err
	}

	if match == nil {
		return fmt.Errorf("no season information found in release name")
	}

	season := match.String()

	for i := 1; i <= numEpisodes; i++ {
		episodeName := strings.ReplaceAll(filepath.Base(dir), season, season+fmt.Sprintf("E%02d", i)) + ".mkv"
		episodePath := filepath.Join(dir, episodeName)

		// Create a minimal file.
		if err = os.WriteFile(episodePath, []byte("0"), 0o644); err != nil {
			return err
		}
	}

	return nil
}

// buildInfoFromPath fills in the name and file list of info by walking root.
// Piece hashes are left empty since mock torrents are only ever parsed, never
// handed to a torrent client.
func buildInfoFromPath(info *metainfo.Info, root string) error {
	info.Name = filepath.Base(root)
	info.Files = nil

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}

		fileInfo, err := entry.Info()
		if err != nil {
			return err
		}

		// A single file torrent carries its length on the info dict itself.
		if path == root {
			info.Length = fileInfo.Size()
			return nil
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		info.Files = append(info.Files, metainfo.FileInfo{
			Path:   strings.Split(relPath, string(filepath.Separator)),
			Length: fileInfo.Size(),
		})

		return nil
	})
	if err != nil {
		return err
	}

	slices.SortFunc(info.Files, func(a, b metainfo.FileInfo) int {
		return cmp.Compare(strings.Join(a.BestPath(), "/"), strings.Join(b.BestPath(), "/"))
	})

	return nil
}

func torrentFromFolder(folderPath string) ([]byte, error) {
	mi := metainfo.MetaInfo{
		AnnounceList: [][]string{},
	}

	info := metainfo.Info{
		PieceLength: 256 * 1024,
	}

	err := buildInfoFromPath(&info, folderPath)
	if err != nil {
		return nil, err
	}

	mi.InfoBytes, err = bencode.Marshal(info)
	if err != nil {
		return nil, err
	}

	torrentBytes := bytes.Buffer{}
	err = mi.Write(&torrentBytes)
	if err != nil {
		return nil, err
	}

	return torrentBytes.Bytes(), nil
}

func TorrentFromRls(rlsName string, numEpisodes int) ([]byte, error) {
	tempDirPath := filepath.Join(os.TempDir(), rlsName)

	// Create the directory with the specified name
	err := os.Mkdir(tempDirPath, os.ModePerm)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tempDirPath) }()

	if err = mockEpisodes(tempDirPath, numEpisodes); err != nil {
		return nil, err
	}

	return torrentFromFolder(tempDirPath)
}
