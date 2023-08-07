package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
