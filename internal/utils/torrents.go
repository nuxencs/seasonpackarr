package utils

import (
	"github.com/anacrolix/torrent/metainfo"
)

func ParseFolderNameFromTorrentFile(path string) (string, error) {
	metaInfo, err := metainfo.LoadFromFile(path)
	if err != nil {
		return "", err
	}

	info, err := metaInfo.UnmarshalInfo()
	if err != nil {
		return "", err
	}

	return info.BestName(), nil
}
