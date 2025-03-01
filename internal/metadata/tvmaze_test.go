// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metadata

import (
	"testing"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/assert"
)

func Test_EpisodesInSeason(t *testing.T) {
	tests := []struct {
		name    string
		release rls.Release
		want    int
		wantErr bool
	}{
		{
			name: "some_show",
			release: rls.Release{
				Title:  "Halo",
				Series: 1,
			},
			want:    9,
			wantErr: false,
		},
		{
			name: "anime_show",
			release: rls.Release{
				Title:  "Attack on Titan",
				Series: 1,
			},
			want:    25,
			wantErr: false,
		},
		{
			name: "season_doesnt_exist",
			release: rls.Release{
				Title:  "Halo",
				Series: 0,
			},
			want:    0,
			wantErr: true,
		},
		{
			name: "show_doesnt_exist",
			release: rls.Release{
				Title:  "Test123",
				Series: 0,
			},
			want:    0,
			wantErr: true,
		},
		{
			name: "some_recent_show",
			release: rls.Release{
				Title:  "Echo",
				Series: 1,
			},
			want:    5,
			wantErr: false,
		},
		{
			name: "show_with_punctuation",
			release: rls.Release{
				Title:  "Orphan Black - Echoes",
				Series: 1,
			},
			want:    10,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EpisodesInSeason(tt.release)

			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.Equalf(t, tt.want, got, "EpisodesInSeason(%s, %d)", tt.release.Title, tt.release.Series)
		})
	}
}
