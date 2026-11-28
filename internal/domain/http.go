// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

import "fmt"

// DefaultSearchLimit is the cap of a search request that omits limit. The CLI
// flag --limit has the same default.
const DefaultSearchLimit = 100

type StatusCode int

const (
	StatusNoMatches               StatusCode = 200
	StatusAlreadyInClient         StatusCode = 210
	StatusNotASeasonPack          StatusCode = 211
	StatusBelowThreshold          StatusCode = 230
	StatusSuccessfulMatch         StatusCode = 250
	StatusSuccessfulHardlink      StatusCode = 250
	StatusFailedHardlink          StatusCode = 440
	StatusFailedMatchToTorrentEps StatusCode = 445
	StatusClientNotFound          StatusCode = 472
	StatusGetClientError          StatusCode = 471
	StatusDecodingError           StatusCode = 470
	StatusAnnounceNameError       StatusCode = 469
	StatusGetTorrentsError        StatusCode = 468
	StatusTorrentBytesError       StatusCode = 467
	StatusDecodeTorrentBytesError StatusCode = 466
	StatusParseTorrentInfoError   StatusCode = 465
	StatusGetEpisodesError        StatusCode = 464
	StatusAddTorrentError         StatusCode = 463
	StatusFindTorrentError        StatusCode = 462
	StatusRecheckTorrentError     StatusCode = 461
	StatusResumeTorrentError      StatusCode = 460
	StatusImportConfigError       StatusCode = 459
)

func (s StatusCode) String() string {
	switch s {
	case StatusNoMatches:
		return "no matching releases in client"
	case StatusAlreadyInClient:
		return "release already in client"
	case StatusNotASeasonPack:
		return "release is not a season pack"
	case StatusBelowThreshold:
		return "number of matches below threshold"
	case StatusSuccessfulMatch:
		return "successful match"
	case StatusFailedHardlink:
		return "could not create hardlinks"
	case StatusFailedMatchToTorrentEps:
		return "could not match episodes to files in pack"
	case StatusClientNotFound:
		return "could not find client in config"
	case StatusGetClientError:
		return "could not get client"
	case StatusDecodingError:
		return "error decoding request"
	case StatusAnnounceNameError:
		return "could not get announce name"
	case StatusGetTorrentsError:
		return "could not get torrents"
	case StatusTorrentBytesError:
		return "could not get torrent bytes"
	case StatusDecodeTorrentBytesError:
		return "could not decode torrent bytes"
	case StatusParseTorrentInfoError:
		return "could not parse torrent info"
	case StatusGetEpisodesError:
		return "could not get episodes"
	case StatusAddTorrentError:
		return "could not add torrent to client"
	case StatusFindTorrentError:
		return "could not find added torrent in client"
	case StatusRecheckTorrentError:
		return "could not recheck torrent in client"
	case StatusResumeTorrentError:
		return "could not resume torrent in client"
	case StatusImportConfigError:
		return "invalid import config"
	default:
		return ""
	}
}

func (s StatusCode) Code() int {
	return int(s)
}

func (s StatusCode) Error() error {
	return fmt.Errorf("%s", s)
}

var NotificationStatusMap = map[string][]StatusCode{
	NotificationLevelMatch: {
		StatusSuccessfulMatch,
	},
	NotificationLevelInfo: {
		StatusNoMatches,
		StatusAlreadyInClient,
		StatusNotASeasonPack,
		StatusBelowThreshold,
	},
	NotificationLevelError: {
		StatusFailedHardlink,
		StatusFailedMatchToTorrentEps,
		StatusClientNotFound,
		StatusGetClientError,
		StatusDecodingError,
		StatusAnnounceNameError,
		StatusGetTorrentsError,
		StatusTorrentBytesError,
		StatusDecodeTorrentBytesError,
		StatusParseTorrentInfoError,
		StatusGetEpisodesError,
		StatusAddTorrentError,
		StatusFindTorrentError,
		StatusRecheckTorrentError,
		StatusResumeTorrentError,
		StatusImportConfigError,
	},
}
