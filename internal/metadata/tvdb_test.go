// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metadata

import (
	"os"
	"testing"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/assert"
)

func Test_TVDB_EpisodesInSeason(t *testing.T) {
	apiKey := os.Getenv("TVDB_API_KEY")
	if apiKey == "" {
		t.Skip("TVDB_API_KEY not set, skipping TVDB tests")
	}

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
			name: "show_with_localized_subtitle",
			release: rls.Release{
				Title:  "NIPPON SANGOKU Die drei Nationen der roten Sonne",
				Series: 1,
			},
			want:    12,
			wantErr: false,
		},
		{
			name: "long_title_not_found_by_tvdb_search",
			release: rls.Release{
				Title:  "My Next Life as a Villainess All Routes Lead to Doom",
				Series: 1,
			},
			want:    12,
			wantErr: false,
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tvdbClient := newTVDB(apiKey, os.Getenv("TVDB_PIN"))

			got, err := tvdbClient.episodesInSeason(tt.release)

			if (err != nil) != tt.wantErr {
				t.Errorf("episodesInSeason() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.Equalf(t, tt.want, got, "TVDB EpisodesInSeason(%s, %d)", tt.release.Title, tt.release.Series)
		})
	}
}
