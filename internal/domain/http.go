// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

import "fmt"

type StatusCode int

const (
	StatusNoMatches                StatusCode = 200
	StatusResolutionMismatch       StatusCode = 201
	StatusSourceMismatch           StatusCode = 202
	StatusRlsGrpMismatch           StatusCode = 203
	StatusCutMismatch              StatusCode = 204
	StatusEditionMismatch          StatusCode = 205
	StatusRepackStatusMismatch     StatusCode = 206
	StatusHdrMismatch              StatusCode = 207
	StatusStreamingServiceMismatch StatusCode = 208
	StatusAlreadyInClient          StatusCode = 210
	StatusNotASeasonPack           StatusCode = 211
	StatusSizeMismatch             StatusCode = 212
	StatusSeasonMismatch           StatusCode = 213
	StatusEpisodeMismatch          StatusCode = 214
	StatusBelowThreshold           StatusCode = 230
	StatusSuccessfulMatch          StatusCode = 250
	StatusSuccessfulHardlink       StatusCode = 250
	StatusFailedHardlink           StatusCode = 440
	StatusFailedMatchToTorrentEps  StatusCode = 445
	StatusClientNotFound           StatusCode = 472
	StatusGetClientError           StatusCode = 471
	StatusDecodingError            StatusCode = 470
	StatusAnnounceNameError        StatusCode = 469
	StatusGetTorrentsError         StatusCode = 468
	StatusTorrentBytesError        StatusCode = 467
	StatusDecodeTorrentBytesError  StatusCode = 466
	StatusParseTorrentInfoError    StatusCode = 465
	StatusGetEpisodesError         StatusCode = 464
	StatusEpisodeCountError        StatusCode = 450
)

func (s StatusCode) String() string {
	switch s {
	case StatusNoMatches:
		return "no matching releases in client"
	case StatusResolutionMismatch:
		return "resolution did not match"
	case StatusSourceMismatch:
		return "source did not match"
	case StatusRlsGrpMismatch:
		return "release group did not match"
	case StatusCutMismatch:
		return "cut did not match"
	case StatusEditionMismatch:
		return "edition did not match"
	case StatusRepackStatusMismatch:
		return "repack status did not match"
	case StatusHdrMismatch:
		return "HDR metadata did not match"
	case StatusStreamingServiceMismatch:
		return "streaming service did not match"
	case StatusAlreadyInClient:
		return "release already in client"
	case StatusNotASeasonPack:
		return "release is not a season pack"
	case StatusSizeMismatch:
		return "size did not match"
	case StatusSeasonMismatch:
		return "season did not match"
	case StatusEpisodeMismatch:
		return "episode did not match"
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
	case StatusEpisodeCountError:
		return "could not get episode count"
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
		StatusResolutionMismatch,
		StatusSourceMismatch,
		StatusRlsGrpMismatch,
		StatusCutMismatch,
		StatusEditionMismatch,
		StatusRepackStatusMismatch,
		StatusHdrMismatch,
		StatusStreamingServiceMismatch,
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
		StatusEpisodeCountError,
	},
}
