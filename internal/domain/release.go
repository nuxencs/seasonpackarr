package domain

import (
	"github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
)

type Entry struct {
	T qbittorrent.Torrent
	R rls.Release
}
