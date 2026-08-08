// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_ShortenedTitles(t *testing.T) {
	tests := []struct {
		name      string
		normTitle string
		want      []string
	}{
		{
			name:      "localized_subtitle",
			normTitle: "nippon sangoku die drei nationen der roten sonne",
			want: []string{
				"nippon sangoku die drei nationen der",
				"nippon sangoku die drei nationen",
				"nippon sangoku die drei",
				"nippon sangoku die",
				"nippon sangoku",
			},
		},
		{
			name:      "very_long_title_capped_at_six_words",
			normTitle: "ive been killing slimes for 300 years and maxed out my level",
			want: []string{
				"ive been killing slimes for 300",
				"ive been killing slimes for",
				"ive been killing slimes",
				"ive been killing",
				"ive been",
			},
		},
		{
			name:      "three_words",
			normTitle: "orphan black echoes",
			want:      []string{"orphan black"},
		},
		{
			name:      "two_words",
			normTitle: "demon slayer",
			want:      nil,
		},
		{
			name:      "single_word",
			normTitle: "halo",
			want:      nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, shortenedTitles(tt.normTitle), "shortenedTitles(%q)", tt.normTitle)
		})
	}
}

func Test_MatchesTitle(t *testing.T) {
	tests := []struct {
		name       string
		normTitle  string
		candidates []string
		want       bool
	}{
		{
			name:       "exact_translation_match",
			normTitle:  "nippon sangoku die drei nationen der roten sonne",
			candidates: []string{"日本三國", "NIPPON SANGOKU: Die drei Nationen der roten Sonne"},
			want:       true,
		},
		{
			name:       "name_is_leading_part_of_title",
			normTitle:  "nippon sangoku die drei nationen der roten sonne",
			candidates: []string{"Nippon Sangoku"},
			want:       true,
		},
		{
			name:       "unrelated_show",
			normTitle:  "nippon sangoku die drei nationen der roten sonne",
			candidates: []string{"Nippon Television Special"},
			want:       false,
		},
		{
			name:       "partial_word_does_not_match",
			normTitle:  "sangokushi the story of three kingdoms",
			candidates: []string{"Sangoku"},
			want:       false,
		},
		{
			name:       "single_word_prefix_does_not_match",
			normTitle:  "nippon sangoku die drei nationen der roten sonne",
			candidates: []string{"Nippon"},
			want:       false,
		},
		{
			name:       "single_word_exact_match",
			normTitle:  "halo",
			candidates: []string{"Halo"},
			want:       true,
		},
		{
			name:       "empty_candidate",
			normTitle:  "some show",
			candidates: []string{""},
			want:       false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, matchesTitle(tt.normTitle, tt.candidates...), "matchesTitle(%q, %v)", tt.normTitle, tt.candidates)
		})
	}
}
