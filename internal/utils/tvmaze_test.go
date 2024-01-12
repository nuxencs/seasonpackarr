package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_GetEpisodesPerSeason(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		season  int
		want    int
		wantErr bool
	}{
		{
			name:    "some_show",
			title:   "Halo",
			season:  1,
			want:    9,
			wantErr: false,
		},
		{
			name:    "anime_show",
			title:   "Attack on Titan",
			season:  1,
			want:    25,
			wantErr: false,
		},
		{
			name:    "season_doesnt_exist",
			title:   "Halo",
			season:  0,
			want:    0,
			wantErr: true,
		},
		{
			name:    "show_doesnt_exist",
			title:   "Test123",
			season:  0,
			want:    0,
			wantErr: true,
		},
		{
			name:    "some_recent_show",
			title:   "Echo",
			season:  1,
			want:    5,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetEpisodesPerSeason(tt.title, tt.season)

			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.Equalf(t, tt.want, got, "GetEpisodesPerSeason(%s, %d)", tt.title, tt.season)
		})
	}
}
