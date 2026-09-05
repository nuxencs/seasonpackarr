// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package format

import (
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/autobrr/rls"
	"github.com/stretchr/testify/assert"
)

func Test_ComparableTitle(t *testing.T) {
	tests := []struct {
		name          string
		packName      string
		fuzzyMatching domain.FuzzyMatching
		want          string
	}{
		{
			name:     "pack_1",
			packName: "Prehistoric Planet 2022 S02 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-FLUX",
			want:     "prehistoric planet20222",
		},
		{
			name:     "pack_2",
			packName: "Rabbit Hole S01 1080p AMZN WEB-DL DDP 5.1 H.264-NTb",
			want:     "rabbit hole01",
		},
		{
			name:     "pack_3",
			packName: "Star Wars Visions S01 REPACK 1080p DSNP WEB-DL DDP 5.1 H.264-FLUX",
			want:     "star wars visions01",
		},
		{
			name:     "pack_4",
			packName: "Star Wars Visions S02 1080p DSNP WEB-DL DDP 5.1 H.264-NTb",
			want:     "star wars visions02",
		},
		{
			name:     "pack_5",
			packName: "The Good Doctor S06 1080p AMZN WEB-DL DDP 5.1 H.264-NTb",
			want:     "the good doctor06",
		},
		{
			name:     "pack_6",
			packName: "The Good Doctor S06 REPACK 1080p AMZN WEB-DL DDP 5.1 H.264-NTb",
			want:     "the good doctor06",
		},
		{
			name:     "pack_7",
			packName: "The Mandalorian S03 1080p DSNP WEB-DL DDP 5.1 Atmos H.264-FLUX",
			want:     "the mandalorian03",
		},
		{
			name:     "pack_8",
			packName: "Gold Rush: White Water S06 1080p AMZN WEB-DL DDP 2.0 H.264-NTb",
			want:     "gold rush white water06",
		},
		{
			name:     "pack_9",
			packName: "Transplant S03 1080p iT WEB-DL AAC 2.0 H.264-NTb",
			want:     "transplant03",
		},
		{
			name:     "pack_10",
			packName: "Mayans M.C. S05 1080p AMZN WEB-DL DDP 5.1 H.264-NTb",
			want:     "mayans m c05",
		},
		{
			name:     "pack_11",
			packName: "What If... S01 1080p DNSP WEB-DL DDP 5.1 H.264-FLUX",
			want:     "what if01",
		},
		{
			name:     "pack_12",
			packName: "Demon Slayer Kimetsu no Yaiba S04 2023 1080p WEB-DL AVC AAC 2.0 Dual Audio -ZR-",
			want:     "demon slayer kimetsu no yaiba20234",
		},
		{
			name:     "pack_13",
			packName: "The Continental 2023 S01 2160p PCOK WEB-DL DDP5.1 Atmos DV HDR H.265-FLUX",
			want:     "the continental20231",
		},
		{
			name:     "pack_14",
			packName: "The Continental 2023 S01 2160p PCOK WEB-DL DDP5.1 Atmos HDR DV H.265-FLUX",
			want:     "the continental20231",
		},
		{
			name:          "pack_skip_year",
			packName:      "The Continental 2023 S01 2160p PCOK WEB-DL DDP5.1 Atmos HDR DV H.265-FLUX",
			fuzzyMatching: domain.FuzzyMatching{SkipYearCompare: true},
			want:          "the continental1",
		},
		{
			name:          "pack_skip_empty_year",
			packName:      "The Continental S01 2160p PCOK WEB-DL DDP5.1 Atmos HDR DV H.265-FLUX",
			fuzzyMatching: domain.FuzzyMatching{SkipYearCompare: true},
			want:          "the continental1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := rls.ParseString(tt.packName)
			assert.Equalf(t, tt.want, ComparableTitle(r, tt.fuzzyMatching), "ComparableTitle(%s)", tt.packName)
		})
	}
}
