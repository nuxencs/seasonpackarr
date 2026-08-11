// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

// Reason identifies why one processing operation produced its outcome. It is
// independent of HTTP status and notification severity.
type Reason string

const (
	ReasonNoMatches                Reason = "no_matches"
	ReasonResolutionMismatch       Reason = "resolution_mismatch"
	ReasonSourceMismatch           Reason = "source_mismatch"
	ReasonReleaseGroupMismatch     Reason = "release_group_mismatch"
	ReasonCutMismatch              Reason = "cut_mismatch"
	ReasonEditionMismatch          Reason = "edition_mismatch"
	ReasonRepackMismatch           Reason = "repack_mismatch"
	ReasonHDRMismatch              Reason = "hdr_mismatch"
	ReasonStreamingServiceMismatch Reason = "streaming_service_mismatch"
	ReasonAlreadyInClient          Reason = "already_in_client"
	ReasonNotSeasonPack            Reason = "not_season_pack"
	ReasonSizeMismatch             Reason = "size_mismatch"
	ReasonSeasonMismatch           Reason = "season_mismatch"
	ReasonEpisodeMismatch          Reason = "episode_mismatch"
	ReasonContainerMismatch        Reason = "container_mismatch"
	ReasonBelowThreshold           Reason = "below_threshold"
	ReasonMatched                  Reason = "matched"
	ReasonImported                 Reason = "imported"
	ReasonHardlinkFailed           Reason = "hardlink_failed"
	ReasonTorrentMismatch          Reason = "torrent_mismatch"
	ReasonClientNotFound           Reason = "client_not_found"
	ReasonClientUnavailable        Reason = "client_unavailable"
	ReasonRequestDecodeFailed      Reason = "request_decode_failed"
	ReasonMissingReleaseName       Reason = "missing_release_name"
	ReasonMissingTorrent           Reason = "missing_torrent"
	ReasonClientInventoryFailed    Reason = "client_inventory_failed"
	ReasonTorrentDecodeFailed      Reason = "torrent_decode_failed"
	ReasonTorrentParseFailed       Reason = "torrent_parse_failed"
	ReasonNoEligibleEpisodes       Reason = "no_eligible_episodes"
	ReasonClientFileInspectFailed  Reason = "client_file_inspection_failed"
	ReasonTorrentAddFailed         Reason = "torrent_add_failed"
	ReasonImportedTorrentNotFound  Reason = "imported_torrent_not_found"
	ReasonTorrentRecheckFailed     Reason = "torrent_recheck_failed"
	ReasonTorrentResumeFailed      Reason = "torrent_resume_failed"
	ReasonImportDestinationFailed  Reason = "import_destination_failed"
	ReasonImportConfigInvalid      Reason = "import_config_invalid"
	ReasonTorrentUnsupported       Reason = "torrent_unsupported"
	ReasonInternalError            Reason = "internal_error"
)

func (r Reason) Message() string {
	switch r {
	case ReasonNoMatches:
		return "no matching releases in client"
	case ReasonResolutionMismatch:
		return "resolution did not match"
	case ReasonSourceMismatch:
		return "source did not match"
	case ReasonReleaseGroupMismatch:
		return "release group did not match"
	case ReasonCutMismatch:
		return "cut did not match"
	case ReasonEditionMismatch:
		return "edition did not match"
	case ReasonRepackMismatch:
		return "repack status did not match"
	case ReasonHDRMismatch:
		return "HDR metadata did not match"
	case ReasonStreamingServiceMismatch:
		return "streaming service did not match"
	case ReasonAlreadyInClient:
		return "release already in client"
	case ReasonNotSeasonPack:
		return "release is not a season pack"
	case ReasonSizeMismatch:
		return "size did not match"
	case ReasonSeasonMismatch:
		return "season did not match"
	case ReasonEpisodeMismatch:
		return "episode did not match"
	case ReasonContainerMismatch:
		return "container did not match"
	case ReasonBelowThreshold:
		return "number of matches below threshold"
	case ReasonMatched:
		return "season pack matched"
	case ReasonImported:
		return "season pack imported"
	case ReasonHardlinkFailed:
		return "could not create hardlinks"
	case ReasonTorrentMismatch:
		return "could not match episodes to files in pack"
	case ReasonClientNotFound:
		return "could not find client in config"
	case ReasonClientUnavailable:
		return "could not get client"
	case ReasonRequestDecodeFailed:
		return "error decoding request"
	case ReasonMissingReleaseName:
		return "could not get announce name"
	case ReasonMissingTorrent:
		return "could not get torrent bytes"
	case ReasonClientInventoryFailed:
		return "could not get torrents"
	case ReasonTorrentDecodeFailed:
		return "could not decode torrent bytes"
	case ReasonTorrentParseFailed:
		return "could not parse torrent info"
	case ReasonNoEligibleEpisodes:
		return "torrent contains no eligible episode files"
	case ReasonClientFileInspectFailed:
		return "could not inspect client episode files"
	case ReasonTorrentAddFailed:
		return "could not add torrent to client"
	case ReasonImportedTorrentNotFound:
		return "could not find added torrent in client"
	case ReasonTorrentRecheckFailed:
		return "could not recheck torrent in client"
	case ReasonTorrentResumeFailed:
		return "could not resume torrent in client"
	case ReasonImportDestinationFailed:
		return "could not resolve import destination"
	case ReasonImportConfigInvalid:
		return "invalid import config"
	case ReasonTorrentUnsupported:
		return "torrent is not supported by selected client"
	case ReasonInternalError:
		return "internal processing error"
	default:
		return ""
	}
}

func (r Reason) Valid() bool {
	return r.Message() != ""
}
