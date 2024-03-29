package torrents

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
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
		episodeName := strings.Replace(filepath.Base(dir), season, season+fmt.Sprintf("E%02d", i), -1) + ".mkv"
		episodePath := filepath.Join(dir, episodeName)

		// Create a minimal file.
		if err = os.WriteFile(episodePath, []byte("0"), 0644); err != nil {
			return err
		}
	}

	return nil
}

func torrentFromFolder(folderPath string) ([]byte, error) {
	mi := metainfo.MetaInfo{
		AnnounceList: [][]string{},
	}

	info := metainfo.Info{
		PieceLength: 256 * 1024,
	}

	err := info.BuildFromFilePath(folderPath)
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
	defer os.RemoveAll(tempDirPath)

	if err = mockEpisodes(tempDirPath, numEpisodes); err != nil {
		return nil, err
	}

	return torrentFromFolder(tempDirPath)
}
