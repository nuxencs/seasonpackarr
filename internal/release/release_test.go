// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package release

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/autobrr/rls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckCandidates_PreservesOriginalFuzzyMismatchValues(t *testing.T) {
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

func TestCheckCandidates_AcceptsNormalizedFuzzyValues(t *testing.T) {
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

func TestSimplifyWEB(t *testing.T) {
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

func TestCheckCandidates_SiloWebTitle(t *testing.T) {
	const packName = "Silo S03 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-FLUX"
	const episodeName = "Silo.S03E03.A.Dark.Web.1080p.ATVP.WEB-DL.DDP5.1.Atmos.H.264-FLUX.mkv"
	const episodeSize = int64(3_961_454_446)

	pack := rls.ParseString(packName)
	episode := rls.ParseString(episodeName)
	result := CheckCandidates(pack, episode, domain.FuzzyMatching{})
	require.Equal(t, domain.StatusSuccessfulMatch, result.StatusCode, "%+v", result)

	target := filepath.Join(packName, episodeName)
	matches := NewEpisodeMatcher([]EpisodeFile{requireEpisodeFile(t, target, episodeSize)}).
		Match([]EpisodeFile{requireEpisodeFile(t, episodeName, episodeSize)}).Matches
	require.Equal(t, []EpisodeMatch{{ClientPath: episodeName, TorrentPath: target}}, matches)

	otherSource := rls.ParseString(strings.Replace(episodeName, "WEB-DL", "WEB", 1))
	result = CheckCandidates(pack, otherSource, domain.FuzzyMatching{})
	require.Equal(t, domain.StatusSourceMismatch, result.StatusCode)
}

func TestCheckCandidates_CodecAttachedToAudio(t *testing.T) {
	// rls before v0.10.0 read "FLAC2.0x265" as episode 265.
	pack := rls.ParseString("Cells.at.Work.S01.1080p.BluRay.FLAC2.0x265-Main10p-CTR")
	require.Equal(t, rls.Series, pack.Type)
	require.Equal(t, 1, pack.Series)
	require.Zero(t, pack.Episode)
	assert.Equal(t, []string{"x265"}, pack.Codec)
	assert.Equal(t, []string{"FLAC"}, pack.Audio)

	episode := rls.ParseString("Cells.at.Work.S01E01.1080p.BluRay.FLAC2.0x265-Main10p-CTR")
	result := CheckCandidates(pack, episode, domain.FuzzyMatching{})
	require.Equal(t, domain.StatusSuccessfulMatch, result.StatusCode, "%+v", result)
}

func TestEqualElements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		x    []string
		y    []string
		want bool
	}{
		{name: "identical elements", x: []string{"a", "b", "c"}, y: []string{"a", "b", "c"}, want: true},
		{name: "different order", x: []string{"a", "b", "c"}, y: []string{"c", "b", "a"}, want: true},
		{name: "different elements", x: []string{"a", "b", "c"}, y: []string{"a", "b", "d"}, want: false},
		{name: "different lengths", x: []string{"a", "b", "c"}, y: []string{"a", "b"}, want: false},
		{name: "same values with other counts", x: []string{"a", "a", "b"}, y: []string{"a", "b", "b"}, want: false},
		{name: "empty slices", x: []string{}, y: []string{}, want: true},
		{name: "one empty slice", x: []string{}, y: []string{"a"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equalf(t, tt.want, equalElements(tt.x, tt.y), "equalElements(%v, %v)", tt.x, tt.y)
		})
	}
}

func TestSimplifyHDR(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{name: "contains HDR", input: []string{"HDR10", "HDR10+", "HDR"}, want: []string{"HDR", "HDR", "HDR"}},
		{name: "no HDR", input: []string{"SDR", "DV"}, want: []string{"SDR", "DV"}},
		{name: "empty slice", input: []string{}, want: []string{}},
		{name: "mixed HDR and others", input: []string{"HDR10", "DV", "SDR", "HDR10+"}, want: []string{"HDR", "DV", "SDR", "HDR"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equalf(t, tt.want, simplifyHDR(tt.input), "simplifyHDR(%v)", tt.input)
		})
	}
}
