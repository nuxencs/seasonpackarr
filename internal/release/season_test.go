// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package release

import (
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/autobrr/rls"
	"github.com/stretchr/testify/assert"
)

// A season key has no readable form, so each case compares the keys of two names.
func TestSeasonKeyOf(t *testing.T) {
	tests := []struct {
		name          string
		a             string
		b             string
		fuzzyMatching domain.FuzzyMatching
		same          bool
	}{
		{
			name: "same show with a year",
			a:    "Prehistoric Planet 2022 S02 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-FLUX",
			b:    "Prehistoric.Planet.2022.S02E01.2160p.ATVP.WEB-DL.DDP5.1.H.265-NTb",
			same: true,
		},
		{
			name: "same show",
			a:    "Rabbit Hole S01 1080p AMZN WEB-DL DDP 5.1 H.264-NTb",
			b:    "Rabbit.Hole.S01E03.720p.AMZN.WEB-DL.DDP5.1.H.264-FLUX",
			same: true,
		},
		{
			name: "repack is the same season",
			a:    "Star Wars Visions S01 REPACK 1080p DSNP WEB-DL DDP 5.1 H.264-FLUX",
			b:    "Star Wars Visions S01 1080p DSNP WEB-DL DDP 5.1 H.264-FLUX",
			same: true,
		},
		{
			name: "other season number",
			a:    "Star Wars Visions S02 1080p DSNP WEB-DL DDP 5.1 H.264-NTb",
			b:    "Star Wars Visions S01 REPACK 1080p DSNP WEB-DL DDP 5.1 H.264-FLUX",
		},
		{
			name: "repack of the same release group",
			a:    "The Good Doctor S06 1080p AMZN WEB-DL DDP 5.1 H.264-NTb",
			b:    "The Good Doctor S06 REPACK 1080p AMZN WEB-DL DDP 5.1 H.264-NTb",
			same: true,
		},
		{
			name: "other show",
			a:    "The Mandalorian S03 1080p DSNP WEB-DL DDP 5.1 Atmos H.264-FLUX",
			b:    "The Good Doctor S03 1080p DSNP WEB-DL DDP 5.1 Atmos H.264-FLUX",
		},
		{
			name: "title punctuation",
			a:    "Gold Rush: White Water S06 1080p AMZN WEB-DL DDP 2.0 H.264-NTb",
			b:    "Gold.Rush.White.Water.S06E01.1080p.AMZN.WEB-DL.DDP2.0.H.264-NTb",
			same: true,
		},
		{
			name: "streaming service is not part of the season",
			a:    "Transplant S03 1080p iT WEB-DL AAC 2.0 H.264-NTb",
			b:    "Transplant S03 1080p AMZN WEB-DL AAC 2.0 H.264-NTb",
			same: true,
		},
		{
			name: "title dots",
			a:    "Mayans M.C. S05 1080p AMZN WEB-DL DDP 5.1 H.264-NTb",
			b:    "Mayans M C S05 1080p AMZN WEB-DL DDP 5.1 H.264-NTb",
			same: true,
		},
		{
			name: "title ellipsis",
			a:    "What If... S01 1080p DNSP WEB-DL DDP 5.1 H.264-FLUX",
			b:    "What If S01 1080p DNSP WEB-DL DDP 5.1 H.264-FLUX",
			same: true,
		},
		{
			name: "year after the season",
			a:    "Demon Slayer Kimetsu no Yaiba S04 2023 1080p WEB-DL AVC AAC 2.0 Dual Audio -ZR-",
			b:    "Demon Slayer Kimetsu no Yaiba 2023 S04 1080p WEB-DL AVC AAC 2.0 Dual Audio -ZR-",
			same: true,
		},
		{
			name: "HDR order is not part of the season",
			a:    "The Continental 2023 S01 2160p PCOK WEB-DL DDP5.1 Atmos DV HDR H.265-FLUX",
			b:    "The Continental 2023 S01 2160p PCOK WEB-DL DDP5.1 Atmos HDR DV H.265-FLUX",
			same: true,
		},
		{
			name: "no year and a year",
			a:    "The Continental S01 2160p PCOK WEB-DL DDP5.1 Atmos HDR DV H.265-FLUX",
			b:    "The Continental 2023 S01 2160p PCOK WEB-DL DDP5.1 Atmos HDR DV H.265-FLUX",
		},
		{
			name: "other year",
			a:    "The Continental 2024 S01 2160p PCOK WEB-DL DDP5.1 Atmos HDR DV H.265-FLUX",
			b:    "The Continental 2023 S01 2160p PCOK WEB-DL DDP5.1 Atmos HDR DV H.265-FLUX",
		},
		{
			name:          "skip year",
			a:             "The Continental 2023 S01 2160p PCOK WEB-DL DDP5.1 Atmos HDR DV H.265-FLUX",
			b:             "The Continental 2024 S01 2160p PCOK WEB-DL DDP5.1 Atmos HDR DV H.265-FLUX",
			fuzzyMatching: domain.FuzzyMatching{SkipYearCompare: true},
			same:          true,
		},
		{
			name:          "skip empty year",
			a:             "The Continental S01 2160p PCOK WEB-DL DDP5.1 Atmos HDR DV H.265-FLUX",
			b:             "The Continental 2023 S01 2160p PCOK WEB-DL DDP5.1 Atmos HDR DV H.265-FLUX",
			fuzzyMatching: domain.FuzzyMatching{SkipYearCompare: true},
			same:          true,
		},
		{
			name:          "skip year keeps the season number",
			a:             "The Continental 2023 S01 2160p PCOK WEB-DL DDP5.1 Atmos HDR DV H.265-FLUX",
			b:             "The Continental 2023 S02 2160p PCOK WEB-DL DDP5.1 Atmos HDR DV H.265-FLUX",
			fuzzyMatching: domain.FuzzyMatching{SkipYearCompare: true},
		},
		{
			name:          "title digits do not join the season number",
			a:             "Show 1 S23 1080p WEB-DL DDP 5.1 H.264-GRP",
			b:             "Show 12 S03 1080p WEB-DL DDP 5.1 H.264-GRP",
			fuzzyMatching: domain.FuzzyMatching{SkipYearCompare: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := SeasonKeyOf(new(rls.ParseString(tt.a)), tt.fuzzyMatching)
			b := SeasonKeyOf(new(rls.ParseString(tt.b)), tt.fuzzyMatching)
			if tt.same {
				assert.Equal(t, a, b, "%s and %s are one season", tt.a, tt.b)
			} else {
				assert.NotEqual(t, a, b, "%s and %s are two seasons", tt.a, tt.b)
			}
		})
	}
}
