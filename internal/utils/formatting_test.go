// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package utils

import (
	"testing"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/assert"
)

func Test_GetFormattedTitle(t *testing.T) {
	tests := []struct {
		name     string
		packName string
		want     string
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := rls.ParseString(tt.packName)
			assert.Equalf(t, tt.want, GetFormattedTitle(r), "FormatSeasonPackTitle(%s)", tt.packName)
		})
	}
}

func Test_FormatSeasonPackTitle(t *testing.T) {
	tests := []struct {
		name     string
		packName string
		want     string
	}{
		{
			name:     "pack_1",
			packName: "Prehistoric Planet 2022 S02 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-FLUX",
			want:     "Prehistoric.Planet.2022.S02.1080p.ATVP.WEB-DL.DDP5.1.Atmos.H.264-FLUX",
		},
		{
			name:     "pack_2",
			packName: "Rabbit Hole S01 1080p AMZN WEB-DL DDP 5.1 H.264-NTb",
			want:     "Rabbit.Hole.S01.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb",
		},
		{
			name:     "pack_3",
			packName: "Star Wars Visions S01 REPACK 1080p DSNP WEB-DL DDP 5.1 H.264-FLUX",
			want:     "Star.Wars.Visions.S01.REPACK.1080p.DSNP.WEB-DL.DDP5.1.H.264-FLUX",
		},
		{
			name:     "pack_4",
			packName: "Star Wars Visions S02 1080p DSNP WEB-DL DDP 5.1 H.264-NTb",
			want:     "Star.Wars.Visions.S02.1080p.DSNP.WEB-DL.DDP5.1.H.264-NTb",
		},
		{
			name:     "pack_5",
			packName: "The Good Doctor S06 1080p AMZN WEB-DL DDP 5.1 H.264-NTb",
			want:     "The.Good.Doctor.S06.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb",
		},
		{
			name:     "pack_6",
			packName: "The Good Doctor S06 REPACK 1080p AMZN WEB-DL DDP 5.1 H.264-NTb",
			want:     "The.Good.Doctor.S06.REPACK.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb",
		},
		{
			name:     "pack_7",
			packName: "The Mandalorian S03 1080p DSNP WEB-DL DDP 5.1 Atmos H.264-FLUX",
			want:     "The.Mandalorian.S03.1080p.DSNP.WEB-DL.DDP5.1.Atmos.H.264-FLUX",
		},
		{
			name:     "pack_8",
			packName: "Gold Rush: White Water S06 1080p AMZN WEB-DL DDP 2.0 H.264-NTb",
			want:     "Gold.Rush.White.Water.S06.1080p.AMZN.WEB-DL.DDP2.0.H.264-NTb",
		},
		{
			name:     "pack_9",
			packName: "Transplant S03 1080p iT WEB-DL AAC 2.0 H.264-NTb",
			want:     "Transplant.S03.1080p.iT.WEB-DL.AAC2.0.H.264-NTb",
		},
		{
			name:     "pack_10",
			packName: "Transplant.S03.1080p.iT.WEB-DL.AAC.2.0.H.264-NTb",
			want:     "Transplant.S03.1080p.iT.WEB-DL.AAC2.0.H.264-NTb",
		},
		{
			name:     "pack_11",
			packName: "Mayans M.C. S05 1080p AMZN WEB-DL DDP 5.1 H.264-NTb",
			want:     "Mayans.M.C.S05.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb",
		},
		{
			name:     "pack_12",
			packName: "What If... S01 1080p DNSP WEB-DL DDP 5.1 H.264-FLUX",
			want:     "What.If.S01.1080p.DNSP.WEB-DL.DDP5.1.H.264-FLUX",
		},
		{
			name:     "pack_13",
			packName: "Demon Slayer Kimetsu no Yaiba S04 2023 1080p WEB-DL AVC AAC 2.0 Dual Audio -ZR-",
			want:     "Demon Slayer Kimetsu no Yaiba S04 2023 1080p WEB-DL AVC AAC 2.0 Dual Audio -ZR-",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, FormatSeasonPackTitle(tt.packName), "FormatSeasonPackTitle(%s)", tt.packName)
		})
	}
}

func Test_ReplaceParentFolder(t *testing.T) {
	tests := []struct {
		name            string
		directory       string
		newParentFolder string
		want            string
	}{
		{
			name:            "directory_file",
			directory:       "/data/torrents/tv-hd/Series.S01.1080p.iP.WEB-DL.AAC2.0.H.264-RlsGrp/Series.S01E03.1080p.iP.WEB-DL.AAC2.0.H.264-RlsGrp.mkv",
			newParentFolder: "Series.S01.1080p.AMZN.WEB-DL.AAC2.0.H.264-RlsGrp",
			want:            "/data/torrents/tv-hd/Series.S01.1080p.AMZN.WEB-DL.AAC2.0.H.264-RlsGrp/Series.S01E03.1080p.iP.WEB-DL.AAC2.0.H.264-RlsGrp.mkv",
		},
		{
			name:            "directory_folder",
			directory:       "/data/torrents/tv-hd/Series.S01.1080p.iP.WEB-DL.AAC2.0.H.264-RlsGrp",
			newParentFolder: "test",
			want:            "/data/torrents/test/Series.S01.1080p.iP.WEB-DL.AAC2.0.H.264-RlsGrp",
		},
		{
			name:            "directory_one_parent",
			directory:       "/data/torrents",
			newParentFolder: "test",
			want:            "/test/torrents",
		},
		{
			name:            "directory_parent",
			directory:       "/data",
			newParentFolder: "test",
			want:            "/data",
		},
		{
			name:            "directory_top_level",
			directory:       "/",
			newParentFolder: "test",
			want:            "/",
		},
		{
			name:            "directory_working",
			directory:       ".",
			newParentFolder: "test",
			want:            ".",
		},
		{
			name:            "file",
			directory:       "test.mkv",
			newParentFolder: "test",
			want:            "test.mkv",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, ReplaceParentFolder(tt.directory, tt.newParentFolder), "ReplaceParentFolder(%s, %s)", tt.directory, tt.newParentFolder)
		})
	}
}

func TestMatchFileNameToSeasonPackNaming(t *testing.T) {
	tests := []struct {
		name         string
		epPathClient string
		torrentEps   []string
		want         string
		wantErr      bool
	}{
		{
			name:         "found_match",
			epPathClient: "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
			torrentEps: []string{
				"Series Title 2022 S02E01 1080p Test ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				"Series Title 2022 S02E02 1080p Test ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
			},
			want:    "Series Title 2022 S02E01 1080p Test ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
			wantErr: false,
		},
		{
			name:         "wrong_episode",
			epPathClient: "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
			torrentEps: []string{
				"Series Title 2022 S02E02 1080p Test ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				"Series Title 2022 S02E03 1080p Test ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
			},
			want:    "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
			wantErr: true,
		},
		{
			name:         "found_no_match",
			epPathClient: "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
			torrentEps:   []string{},
			want:         "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
			wantErr:      true,
		},
		{
			name:         "wrong_season",
			epPathClient: "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
			torrentEps: []string{
				"Series Title 2022 S03E01 1080p Test ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				"Series Title 2022 S03E02 1080p Test ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
			},
			want:    "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
			wantErr: true,
		},
		{
			name:         "wrong_resolution",
			epPathClient: "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
			torrentEps: []string{
				"Series Title 2022 S02E01 720p Test ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				"Series Title 2022 S02E02 1080p Test ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
			},
			want:    "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
			wantErr: true,
		},
		{
			name:         "wrong_rlsgrp",
			epPathClient: "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
			torrentEps: []string{
				"Series Title 2022 S02E01 1080p Test ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp2.mkv",
				"Series Title 2022 S02E02 1080p Test ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
			},
			want:    "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MatchFileNameToSeasonPackNaming(tt.epPathClient, tt.torrentEps)

			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.Equalf(t, tt.want, got, "MatchFileNameToSeasonPackNaming(%v, %v)", tt.epPathClient, tt.torrentEps)
		})
	}
}
