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

func TestNewSeasonPack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		release string
		want    bool
	}{
		{name: "single episode", release: "Show.S01E01.1080p.WEB-DL.H.264-GRP"},
		{name: "file extension", release: "Show.S01.1080p.WEB-DL.H.264-GRP.mkv"},
		{name: "season pack", release: "Show.S01.1080p.WEB-DL.H.264-GRP", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, ok := NewSeasonPack(new(rls.ParseString(tt.release)), domain.FuzzyMatching{})
			assert.Equal(t, tt.want, ok)
		})
	}
}

func TestSeasonPackCompare(t *testing.T) {
	t.Parallel()

	// Each case changes the season pack and the other release from one base
	// release variant. The other release is episode 1 unless the case changes it.
	tests := []struct {
		name          string
		pack          func(*rls.Release)
		other         func(*rls.Release)
		fuzzyMatching domain.FuzzyMatching
		want          Relation
		mismatch      Mismatch
	}{
		{name: "compatible", want: Compatible},
		{name: "same pack", other: func(r *rls.Release) { r.Episode = 0 }, want: SamePack},
		// An S01E00 episode has the episode number of a season pack, but it is one episode and not the pack.
		{name: "episode zero", other: func(r *rls.Release) { r.Type, r.Episode = rls.Episode, 0 }, want: Compatible},
		{name: "other season number", other: func(r *rls.Release) { r.Series = 2 }, want: OtherSeason},
		// The compare checks the season itself, so a caller cannot get "same pack" for another show.
		{name: "other show with the same release variant", other: func(r *rls.Release) { r.Title, r.Episode = "Other Show", 0 }, want: OtherSeason},
		{name: "title spelling", other: func(r *rls.Release) { r.Title = "show" }, want: Compatible},
		{name: "no year and a year", other: func(r *rls.Release) { r.Year = 2023 }, want: OtherSeason},
		{name: "a year and no year", pack: func(r *rls.Release) { r.Year = 2023 }, want: OtherSeason},
		{name: "other year", pack: func(r *rls.Release) { r.Year = 2024 }, other: func(r *rls.Release) { r.Year = 2023 }, want: OtherSeason},
		{name: "skip year in the other release", other: func(r *rls.Release) { r.Year = 2023 }, fuzzyMatching: domain.FuzzyMatching{SkipYearCompare: true}, want: Compatible},
		{name: "skip year in the season pack", pack: func(r *rls.Release) { r.Year = 2023 }, fuzzyMatching: domain.FuzzyMatching{SkipYearCompare: true}, want: Compatible},
		{
			name:     "resolution",
			other:    func(r *rls.Release) { r.Resolution = "2160p" },
			want:     OtherReleaseVariant,
			mismatch: Mismatch{Field: MismatchResolution, Want: "1080p", Got: "2160p"},
		},
		{
			name:     "first property that differs",
			other:    func(r *rls.Release) { r.Resolution, r.Source, r.Collection = "2160p", "BluRay", "NF" },
			want:     OtherReleaseVariant,
			mismatch: Mismatch{Field: MismatchResolution, Want: "1080p", Got: "2160p"},
		},
		{
			name:     "source",
			other:    func(r *rls.Release) { r.Source = "WEB" },
			want:     OtherReleaseVariant,
			mismatch: Mismatch{Field: MismatchSource, Want: "WEB-DL", Got: "WEB"},
		},
		{name: "simplify web in the other release", pack: func(r *rls.Release) { r.Source = "WEB" }, fuzzyMatching: domain.FuzzyMatching{SimplifyWebCompare: true}, want: Compatible},
		{name: "simplify web in the season pack", other: func(r *rls.Release) { r.Source = "WEB" }, fuzzyMatching: domain.FuzzyMatching{SimplifyWebCompare: true}, want: Compatible},
		{
			name:          "simplify web keeps WEBRiP",
			other:         func(r *rls.Release) { r.Source = "WEBRiP" },
			fuzzyMatching: domain.FuzzyMatching{SimplifyWebCompare: true},
			want:          OtherReleaseVariant,
			mismatch:      Mismatch{Field: MismatchSource, Want: "WEB-DL", Got: "WEBRiP"},
		},
		{
			name:          "simplify web reports the original source",
			other:         func(r *rls.Release) { r.Source = "BluRay" },
			fuzzyMatching: domain.FuzzyMatching{SimplifyWebCompare: true},
			want:          OtherReleaseVariant,
			mismatch:      Mismatch{Field: MismatchSource, Want: "WEB-DL", Got: "BluRay"},
		},
		{
			name:     "release group",
			other:    func(r *rls.Release) { r.Group = "OTHER" },
			want:     OtherReleaseVariant,
			mismatch: Mismatch{Field: MismatchReleaseGroup, Want: "GRP", Got: "OTHER"},
		},
		{name: "release group spelling", other: func(r *rls.Release) { r.Group = "grp" }, want: Compatible},
		{
			name:     "cut",
			other:    func(r *rls.Release) { r.Cut = []string{"Extended"} },
			want:     OtherReleaseVariant,
			mismatch: Mismatch{Field: MismatchCut, Want: []string(nil), Got: []string{"Extended"}},
		},
		{
			name:     "edition",
			pack:     func(r *rls.Release) { r.Edition = []string{"Remastered"} },
			want:     OtherReleaseVariant,
			mismatch: Mismatch{Field: MismatchEdition, Want: []string{"Remastered"}, Got: []string(nil)},
		},
		{
			name:     "repack status",
			other:    func(r *rls.Release) { r.Other = []string{"REPACK"} },
			want:     OtherReleaseVariant,
			mismatch: Mismatch{Field: MismatchRepackStatus, Want: []string(nil), Got: []string{"REPACK"}},
		},
		{name: "skip repack in the other release", other: func(r *rls.Release) { r.Other = []string{"REPACK"} }, fuzzyMatching: domain.FuzzyMatching{SkipRepackCompare: true}, want: Compatible},
		{name: "skip repack in the season pack", pack: func(r *rls.Release) { r.Other = []string{"REPACK"} }, fuzzyMatching: domain.FuzzyMatching{SkipRepackCompare: true}, want: Compatible},
		{
			name:     "HDR",
			pack:     func(r *rls.Release) { r.HDR = []string{"HDR10"} },
			other:    func(r *rls.Release) { r.HDR = []string{"HDR10+"} },
			want:     OtherReleaseVariant,
			mismatch: Mismatch{Field: MismatchHDR, Want: []string{"HDR10"}, Got: []string{"HDR10+"}},
		},
		{
			name:          "simplify HDR in the other release",
			pack:          func(r *rls.Release) { r.HDR = []string{"HDR"} },
			other:         func(r *rls.Release) { r.HDR = []string{"HDR10+"} },
			fuzzyMatching: domain.FuzzyMatching{SimplifyHdrCompare: true},
			want:          Compatible,
		},
		{
			name:          "simplify HDR in the season pack",
			pack:          func(r *rls.Release) { r.HDR = []string{"HDR10", "DV"} },
			other:         func(r *rls.Release) { r.HDR = []string{"DV", "HDR"} },
			fuzzyMatching: domain.FuzzyMatching{SimplifyHdrCompare: true},
			want:          Compatible,
		},
		{
			name:          "simplify HDR reports the original formats",
			pack:          func(r *rls.Release) { r.HDR = []string{"HDR10"} },
			other:         func(r *rls.Release) { r.HDR = []string{"DV"} },
			fuzzyMatching: domain.FuzzyMatching{SimplifyHdrCompare: true},
			want:          OtherReleaseVariant,
			mismatch:      Mismatch{Field: MismatchHDR, Want: []string{"HDR10"}, Got: []string{"DV"}},
		},
		{
			name:     "streaming service",
			pack:     func(r *rls.Release) { r.Collection = "AMZN" },
			other:    func(r *rls.Release) { r.Collection = "NF" },
			want:     OtherReleaseVariant,
			mismatch: Mismatch{Field: MismatchStreamingService, Want: "AMZN", Got: "NF"},
		},
		{
			name:  "list in another order",
			pack:  func(r *rls.Release) { r.Cut, r.HDR = []string{"Extended", "Uncut"}, []string{"DV", "HDR"} },
			other: func(r *rls.Release) { r.Cut, r.HDR = []string{"Uncut", "Extended"}, []string{"HDR", "DV"} },
			want:  Compatible,
		},
		{
			name:     "list with a repeated value",
			pack:     func(r *rls.Release) { r.Edition = []string{"Remastered", "Remastered", "IMAX"} },
			other:    func(r *rls.Release) { r.Edition = []string{"Remastered", "IMAX", "IMAX"} },
			want:     OtherReleaseVariant,
			mismatch: Mismatch{Field: MismatchEdition, Want: []string{"Remastered", "Remastered", "IMAX"}, Got: []string{"Remastered", "IMAX", "IMAX"}},
		},
		{
			name:     "list with another length",
			pack:     func(r *rls.Release) { r.HDR = []string{"DV"} },
			other:    func(r *rls.Release) { r.HDR = []string{"DV", "HDR"} },
			want:     OtherReleaseVariant,
			mismatch: Mismatch{Field: MismatchHDR, Want: []string{"DV"}, Got: []string{"DV", "HDR"}},
		},
		{name: "codec and audio are not part of the release variant", other: func(r *rls.Release) { r.Codec, r.Audio = []string{"x265"}, []string{"FLAC"} }, want: Compatible},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			build := func(episode int, change func(*rls.Release)) *rls.Release {
				r := &rls.Release{Type: rls.Series, Title: "Show", Series: 1, Episode: episode, Resolution: "1080p", Source: "WEB-DL", Group: "GRP"}
				if change != nil {
					change(r)
				}
				return r
			}
			pack, ok := NewSeasonPack(build(0, tt.pack), tt.fuzzyMatching)
			require.True(t, ok)
			other := build(1, tt.other)

			relation, mismatch := pack.Compare(other)
			assert.Equal(t, tt.want, relation)
			assert.Equal(t, tt.mismatch, mismatch)

			// A fuzzy matching setting must change neither release.
			assert.Equal(t, build(1, tt.other), other)
			relation, mismatch = pack.Compare(build(1, tt.other))
			assert.Equal(t, tt.want, relation, "second compare")
			assert.Equal(t, tt.mismatch, mismatch, "second compare")
		})
	}
}

func TestSeasonPackCompare_SiloWebTitle(t *testing.T) {
	const packName = "Silo S03 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-FLUX"
	const episodeName = "Silo.S03E03.A.Dark.Web.1080p.ATVP.WEB-DL.DDP5.1.Atmos.H.264-FLUX.mkv"
	const episodeSize = int64(3_961_454_446)

	pack, ok := NewSeasonPack(new(rls.ParseString(packName)), domain.FuzzyMatching{})
	require.True(t, ok)
	relation, mismatch := pack.Compare(new(rls.ParseString(episodeName)))
	require.Equal(t, Compatible, relation, "%+v", mismatch)

	target := filepath.Join(packName, episodeName)
	matches := NewEpisodeMatcher([]EpisodeFile{requireEpisodeFile(t, target, episodeSize)}).
		Match([]EpisodeFile{requireEpisodeFile(t, episodeName, episodeSize)}).Matches
	require.Equal(t, []EpisodeMatch{{ClientPath: episodeName, TorrentPath: target}}, matches)

	relation, mismatch = pack.Compare(new(rls.ParseString(strings.Replace(episodeName, "WEB-DL", "WEB", 1))))
	require.Equal(t, OtherReleaseVariant, relation)
	require.Equal(t, MismatchSource, mismatch.Field)
}

func TestSeasonPackCompare_CodecAttachedToAudio(t *testing.T) {
	// rls before v0.10.0 read "FLAC2.0x265" as episode 265.
	parsed := rls.ParseString("Cells.at.Work.S01.1080p.BluRay.FLAC2.0x265-Main10p-CTR")
	require.Equal(t, rls.Series, parsed.Type)
	require.Equal(t, 1, parsed.Series)
	require.Zero(t, parsed.Episode)
	assert.Equal(t, []string{"x265"}, parsed.Codec)
	assert.Equal(t, []string{"FLAC"}, parsed.Audio)

	pack, ok := NewSeasonPack(&parsed, domain.FuzzyMatching{})
	require.True(t, ok)
	relation, mismatch := pack.Compare(new(rls.ParseString("Cells.at.Work.S01E01.1080p.BluRay.FLAC2.0x265-Main10p-CTR")))
	require.Equal(t, Compatible, relation, "%+v", mismatch)
}
