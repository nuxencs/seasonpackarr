// Copyright (c) 2021 - 2024, Ludvig Lundgren and the autobrr contributors.
// Code is heavily modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package notification

import (
	"github.com/nuxencs/seasonpackarr/internal/domain"
)

// BuildTitle constructs the title of the notification message.
func BuildTitle(event int) string {
	titles := map[int]string{
		domain.StatusNoMatches:                "No Matches",
		domain.StatusResolutionMismatch:       "Resolution Mismatch",
		domain.StatusSourceMismatch:           "Source Mismatch",
		domain.StatusRlsGrpMismatch:           "Release Group Mismatch",
		domain.StatusCutMismatch:              "Cut Mismatch",
		domain.StatusEditionMismatch:          "Edition Mismatch",
		domain.StatusRepackStatusMismatch:     "Repack Status Mismatch",
		domain.StatusHdrMismatch:              "HDR Mismatch",
		domain.StatusStreamingServiceMismatch: "Streaming Service Mismatch",
		domain.StatusAlreadyInClient:          "Already In Client",
		domain.StatusNotASeasonPack:           "Not A Season Pack",
		domain.StatusBelowThreshold:           "Below Threshold",
		domain.StatusSuccessfulMatch:          "Success!", // same title for StatusSuccessfulHardlink
		domain.StatusFailedHardlink:           "Failed Hardlink",
		domain.StatusClientNotFound:           "Client Not Found",
		domain.StatusGetClientError:           "Get Client Error",
		domain.StatusDecodingError:            "Decoding Error",
		domain.StatusAnnounceNameError:        "Announce Name Error",
		domain.StatusGetTorrentsError:         "Get Torrents Error",
		domain.StatusTorrentBytesError:        "Torrent Bytes Error",
		domain.StatusDecodeTorrentBytesError:  "Torrent Bytes Decoding Error",
		domain.StatusParseTorrentInfoError:    "Torrent Parsing Error",
		domain.StatusGetEpisodesError:         "Get Episodes Error",
		domain.StatusEpisodeCountError:        "Episode Count Error",
	}

	if title, ok := titles[event]; ok {
		return title
	}

	return "New Event"
}
