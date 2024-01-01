package utils

import (
	"github.com/anacrolix/torrent/metainfo"
)

func ParseFolderNameFromTorrentFile(path string) (string, error) {
	v, _ := metainfo.LoadFromFile(path)
	meta, err := v.UnmarshalInfo()
	if err != nil {
		return "", err
	}
	return meta.BestName(), nil
}
