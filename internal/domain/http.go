// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

const (
	StatusNoMatches                = 200
	StatusResolutionMismatch       = 201
	StatusSourceMismatch           = 202
	StatusRlsGrpMismatch           = 203
	StatusCutMismatch              = 204
	StatusEditionMismatch          = 205
	StatusRepackStatusMismatch     = 206
	StatusHdrMismatch              = 207
	StatusStreamingServiceMismatch = 208
	StatusAlreadyInClient          = 210
	StatusNotASeasonPack           = 211
	StatusBelowThreshold           = 230
	StatusSuccessfulMatch          = 250
	StatusSuccessfulHardlink       = 250
	StatusFailedHardlink           = 440
	StatusClientNotFound           = 472
	StatusGetClientError           = 471
	StatusDecodingError            = 470
	StatusAnnounceNameError        = 469
	StatusGetTorrentsError         = 468
	StatusTorrentBytesError        = 467
	StatusDecodeTorrentBytesError  = 466
	StatusParseTorrentInfoError    = 465
	StatusGetEpisodesError         = 464
	StatusEpisodeCountError        = 450
)
