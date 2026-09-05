// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package release

import (
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/autobrr/rls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckCandidatesPreservesOriginalFuzzyMismatchValues(t *testing.T) {
	t.Parallel()

	t.Run("source", func(t *testing.T) {
		t.Parallel()

		result := CheckCandidates(
			rls.Release{Type: rls.Series, Source: "WEB-DL"},
			rls.Release{Type: rls.Episode, Source: "BluRay"},
			domain.FuzzyMatching{SimplifyWebCompare: true},
		)

		require.Equal(t, domain.StatusSourceMismatch, result.StatusCode)
		require.Equal(t, "WEB-DL", result.RejectValueA)
		require.Equal(t, "BluRay", result.RejectValueB)
	})

	t.Run("HDR", func(t *testing.T) {
		t.Parallel()

		request := rls.Release{Type: rls.Series, HDR: []string{"HDR10"}}
		client := rls.Release{Type: rls.Episode, HDR: []string{"DV"}}
		result := CheckCandidates(request, client, domain.FuzzyMatching{SimplifyHdrCompare: true})

		require.Equal(t, domain.StatusHdrMismatch, result.StatusCode)
		require.Equal(t, []string{"HDR10"}, result.RejectValueA)
		require.Equal(t, []string{"DV"}, result.RejectValueB)
		require.Equal(t, []string{"HDR10"}, request.HDR)
		require.Equal(t, []string{"DV"}, client.HDR)
	})
}

func TestCheckCandidatesStillAcceptsNormalizedFuzzyValues(t *testing.T) {
	t.Parallel()

	t.Run("source", func(t *testing.T) {
		t.Parallel()

		result := CheckCandidates(
			rls.Release{Type: rls.Series, Source: "WEB-DL"},
			rls.Release{Type: rls.Episode, Episode: 1, Source: "WEB"},
			domain.FuzzyMatching{SimplifyWebCompare: true},
		)

		require.Equal(t, domain.StatusSuccessfulMatch, result.StatusCode)
	})

	t.Run("HDR", func(t *testing.T) {
		t.Parallel()

		result := CheckCandidates(
			rls.Release{Type: rls.Series, HDR: []string{"HDR10"}},
			rls.Release{Type: rls.Episode, Episode: 1, HDR: []string{"HDR10+"}},
			domain.FuzzyMatching{SimplifyHdrCompare: true},
		)

		require.Equal(t, domain.StatusSuccessfulMatch, result.StatusCode)
	})
}

func Test_MatchEpToSeasonPackEp(t *testing.T) {
	type args struct {
		clientEpPath  string
		clientEpSize  int64
		torrentEpPath string
		torrentEpSize int64
	}

	type compare struct {
		path string
		info domain.CompareInfo
	}

	tests := []struct {
		name string
		args args
		want compare
	}{
		{
			name: "found_match",
			args: args{
				clientEpPath:  "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				clientEpSize:  2316560346,
				torrentEpPath: "Series Title 2022 S02E01 1080p Test ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				torrentEpSize: 2316560346,
			},
			want: compare{
				path: "Series Title 2022 S02E01 1080p Test ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				info: domain.CompareInfo{},
			},
		},
		{
			name: "wrong_episode",
			args: args{
				clientEpPath:  "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				clientEpSize:  2316560346,
				torrentEpPath: "Series Title 2022 S02E02 1080p Test ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				torrentEpSize: 2316560346,
			},
			want: compare{
				path: "",
				info: domain.CompareInfo{
					StatusCode:   domain.StatusEpisodeMismatch,
					RejectValueA: 1,
					RejectValueB: 2,
				},
			},
		},
		{
			name: "wrong_season",
			args: args{
				clientEpPath:  "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				clientEpSize:  2316560346,
				torrentEpPath: "Series Title 2022 S03E01 1080p Test ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				torrentEpSize: 2316560346,
			},
			want: compare{
				path: "",
				info: domain.CompareInfo{
					StatusCode:   domain.StatusSeasonMismatch,
					RejectValueA: 2,
					RejectValueB: 3,
				},
			},
		},
		{
			name: "wrong_resolution",
			args: args{
				clientEpPath:  "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				clientEpSize:  2316560346,
				torrentEpPath: "Series Title 2022 S02E01 2160p Test ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				torrentEpSize: 2316560346,
			},
			want: compare{
				path: "",
				info: domain.CompareInfo{
					StatusCode:   domain.StatusResolutionMismatch,
					RejectValueA: "1080p",
					RejectValueB: "2160p",
				},
			},
		},
		{
			name: "wrong_rlsgrp",
			args: args{
				clientEpPath:  "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				clientEpSize:  2316560346,
				torrentEpPath: "Series Title 2022 S02E01 1080p Test ATVP WEB-DL DDP 5.1 Atmos H.264-OtherRlsGrp.mkv",
				torrentEpSize: 2316560346,
			},
			want: compare{
				path: "",
				info: domain.CompareInfo{
					StatusCode:   domain.StatusRlsGrpMismatch,
					RejectValueA: "RlsGrp",
					RejectValueB: "OtherRlsGrp",
				},
			},
		},
		{
			name: "wrong_size",
			args: args{
				clientEpPath:  "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				clientEpSize:  2316560346,
				torrentEpPath: "Series Title 2022 S02E01 1080p Test ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				torrentEpSize: 2278773077,
			},
			want: compare{
				path: "",
				info: domain.CompareInfo{
					StatusCode:   domain.StatusSizeMismatch,
					RejectValueA: int64(2316560346),
					RejectValueB: int64(2278773077),
				},
			},
		},
		{
			name: "wrong_container",
			args: args{
				clientEpPath:  "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				clientEpSize:  2316560346,
				torrentEpPath: "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mp4",
				torrentEpSize: 2316560346,
			},
			want: compare{
				path: "",
				info: domain.CompareInfo{
					StatusCode:   domain.StatusContainerMismatch,
					RejectValueA: "mkv",
					RejectValueB: "mp4",
				},
			},
		},
		{
			name: "subfolder_in_client",
			args: args{
				clientEpPath:  "Test/Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				clientEpSize:  2316560346,
				torrentEpPath: "Series Title 2022 S02E01 Test 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				torrentEpSize: 2316560346,
			},
			want: compare{
				path: "Series Title 2022 S02E01 Test 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				info: domain.CompareInfo{},
			},
		},
		{
			name: "subfolder_in_torrent",
			args: args{
				clientEpPath:  "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				clientEpSize:  2316560346,
				torrentEpPath: "Test/Series Title 2022 S02E01 Test 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				torrentEpSize: 2316560346,
			},
			want: compare{
				path: "Test/Series Title 2022 S02E01 Test 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				info: domain.CompareInfo{},
			},
		},
		{
			name: "subfolder_in_both",
			args: args{
				clientEpPath:  "Test/Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				clientEpSize:  2316560346,
				torrentEpPath: "Test/Series Title 2022 S02E01 Test 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				torrentEpSize: 2316560346,
			},
			want: compare{
				path: "Test/Series Title 2022 S02E01 Test 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				info: domain.CompareInfo{},
			},
		},
		{
			name: "multi_subfolder",
			args: args{
				clientEpPath:  "/data/torrents/tv/Test/Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				clientEpSize:  2316560346,
				torrentEpPath: "Series Title 2022 S02/Test/Series Title 2022 S02E01 Test 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				torrentEpSize: 2316560346,
			},
			want: compare{
				path: "Series Title 2022 S02/Test/Series Title 2022 S02E01 Test 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				info: domain.CompareInfo{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, gotInfo := MatchEpToSeasonPackEp(tt.args.clientEpPath, tt.args.clientEpSize, tt.args.torrentEpPath, tt.args.torrentEpSize)

			got := compare{
				path: gotPath,
				info: gotInfo,
			}

			assert.Equalf(t, tt.want, got, "MatchEpToSeasonPackEp(%v, %v, %v, %v)",
				tt.args.clientEpPath, tt.args.clientEpSize, tt.args.torrentEpPath, tt.args.torrentEpSize)
		})
	}
}

func Test_IsValidEpisodeFile(t *testing.T) {
	type args struct {
		torrentFileName string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "sample_with_dash",
			args: args{
				torrentFileName: "test.release.s06e03.dutch.1080p.web.h264-rlsgrp-sample.mkv",
			},
			want: false,
		},
		{
			name: "sample_with_dot",
			args: args{
				torrentFileName: "test.release.s06e03.dutch.1080p.web.h264-rlsgrp.sample.mkv",
			},
			want: false,
		},
		{
			name: "wrong_ext",
			args: args{
				torrentFileName: "test.release.s06e03.dutch.1080p.web.h264-rlsgrp.nfo",
			},
			want: false,
		},
		{
			name: "wrong_ext_and_sample",
			args: args{
				torrentFileName: "test.release.s06e03.dutch.1080p.web.h264-rlsgrp.sample.nfo",
			},
			want: false,
		},
		{
			name: "extra_video",
			args: args{
				torrentFileName: "Extras/Interview.1080p.WEB-DL.mkv",
			},
			want: false,
		},
		{
			name: "special_episode",
			args: args{
				torrentFileName: "test.release.s00e01.dutch.1080p.web.h264-rlsgrp.mkv",
			},
			want: true,
		},
		{
			name: "valid_release",
			args: args{
				torrentFileName: "test.release.s06e03.dutch.1080p.web.h264-rlsgrp.mkv",
			},
			want: true,
		},
		{
			name: "valid_mp4_release",
			args: args{
				torrentFileName: "test.release.s06e03.dutch.1080p.web.h264-rlsgrp.MP4",
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, IsValidEpisodeFile(tt.args.torrentFileName), "IsValidEpisodeFile(%v)", tt.args.torrentFileName)
		})
	}
}

func Test_SimplifyWEB(t *testing.T) {
	type args struct {
		source string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "web-dl",
			args: args{
				source: "WEB-DL",
			},
			want: "WEB",
		},
		{
			name: "webrip",
			args: args{
				source: "WEBRiP",
			},
			want: "WEBRiP",
		},
		{
			name: "bluray",
			args: args{
				source: "BluRay",
			},
			want: "BluRay",
		},
		{
			name: "empty",
			args: args{
				source: "",
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, simplifyWEB(tt.args.source), "simplifyWEB(%v)", tt.args.source)
		})
	}
}
