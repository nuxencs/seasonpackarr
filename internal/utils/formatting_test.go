package utils

import (
	"github.com/moistari/rls"
	"testing"

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
			want:     "prehistoric planet202221080pweb-dlfluxatvp",
		},
		{
			name:     "pack_2",
			packName: "Rabbit Hole S01 1080p AMZN WEB-DL DDP 5.1 H.264-NTb",
			want:     "rabbit hole011080pweb-dlntbamzn",
		},
		{
			name:     "pack_3",
			packName: "Star Wars Visions S01 REPACK 1080p DSNP WEB-DL DDP 5.1 H.264-FLUX",
			want:     "star wars visions011080pweb-dlfluxrepackdsnp",
		},
		{
			name:     "pack_4",
			packName: "Star Wars Visions S02 1080p DSNP WEB-DL DDP 5.1 H.264-NTb",
			want:     "star wars visions021080pweb-dlntbdsnp",
		},
		{
			name:     "pack_5",
			packName: "The Good Doctor S06 1080p AMZN WEB-DL DDP 5.1 H.264-NTb",
			want:     "the good doctor061080pweb-dlntbamzn",
		},
		{
			name:     "pack_6",
			packName: "The Good Doctor S06 REPACK 1080p AMZN WEB-DL DDP 5.1 H.264-NTb",
			want:     "the good doctor061080pweb-dlntbrepackamzn",
		},
		{
			name:     "pack_7",
			packName: "The Mandalorian S03 1080p DSNP WEB-DL DDP 5.1 Atmos H.264-FLUX",
			want:     "the mandalorian031080pweb-dlfluxdsnp",
		},
		{
			name:     "pack_8",
			packName: "Gold Rush: White Water S06 1080p AMZN WEB-DL DDP 2.0 H.264-NTb",
			want:     "gold rush white water061080pweb-dlntbamzn",
		},
		{
			name:     "pack_9",
			packName: "Transplant S03 1080p iT WEB-DL AAC 2.0 H.264-NTb",
			want:     "transplant031080pweb-dlntbit",
		},
		{
			name:     "pack_10",
			packName: "Mayans M.C. S05 1080p AMZN WEB-DL DDP 5.1 H.264-NTb",
			want:     "mayans m c051080pweb-dlntbamzn",
		},
		{
			name:     "pack_11",
			packName: "What If... S01 1080p DNSP WEB-DL DDP 5.1 H.264-FLUX",
			want:     "what if011080pweb-dlfluxdnsp",
		},
		{
			name:     "pack_12",
			packName: "Demon Slayer Kimetsu no Yaiba S04 2023 1080p WEB-DL AVC AAC 2.0 Dual Audio -ZR-",
			want:     "demon slayer kimetsu no yaiba202341080pweb-dlzr",
		},
		{
			name:     "pack_13",
			packName: "The Continental 2023 S01 2160p PCOK WEB-DL DDP5.1 Atmos DV HDR H.265-FLUX",
			want:     "the continental202312160pweb-dlfluxdvhdrpcok",
		},
		{
			name:     "pack_14",
			packName: "The Continental 2023 S01 2160p PCOK WEB-DL DDP5.1 Atmos HDR DV H.265-FLUX",
			want:     "the continental202312160pweb-dlfluxdvhdrpcok",
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
