// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package slices

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDedupe(t *testing.T) {
	tests := []struct {
		name  string
		slice any
		want  any
	}{
		{
			name:  "string slice some duplicates",
			slice: []string{"string_1", "string_2", "string_3", "string_2", "string_1"},
			want:  []string{"string_1", "string_2", "string_3"},
		},
		{
			name:  "string slice all duplicates",
			slice: []string{"string_1", "string_1", "string_1", "string_1", "string_1"},
			want:  []string{"string_1"},
		},
		{
			name:  "string slice no duplicates",
			slice: []string{"string_1", "string_2", "string_3"},
			want:  []string{"string_1", "string_2", "string_3"},
		},
		{
			name:  "string slice empty",
			slice: []string{},
			want:  []string{},
		},
		{
			name:  "int slice some duplicates",
			slice: []int{1, 2, 3, 2, 1},
			want:  []int{1, 2, 3},
		},
		{
			name:  "int slice all duplicates",
			slice: []int{1, 1, 1, 1, 1},
			want:  []int{1},
		},
		{
			name:  "int slice no duplicates",
			slice: []int{1, 2, 3},
			want:  []int{1, 2, 3},
		},
		{
			name:  "int slice empty",
			slice: []int{},
			want:  []int{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch v := tt.slice.(type) {
			case []string:
				assert.ElementsMatchf(t, tt.want, Dedupe(v), "Dedupe(%v)", v)
			case []int:
				assert.ElementsMatchf(t, tt.want, Dedupe(v), "Dedupe(%v)", v)
			default:
				t.Errorf("Unsupported slice type in test case: %v", tt.name)
			}
		})
	}
}

func TestEqualElements(t *testing.T) {
	tests := []struct {
		name string
		x    any
		y    any
		want bool
	}{
		{
			name: "string slice identical elements",
			x:    []string{"a", "b", "c"},
			y:    []string{"a", "b", "c"},
			want: true,
		},
		{
			name: "string slice different order",
			x:    []string{"a", "b", "c"},
			y:    []string{"c", "b", "a"},
			want: true,
		},
		{
			name: "string slice different elements",
			x:    []string{"a", "b", "c"},
			y:    []string{"a", "b", "d"},
			want: false,
		},
		{
			name: "string slice different lengths",
			x:    []string{"a", "b", "c"},
			y:    []string{"a", "b"},
			want: false,
		},
		{
			name: "int slice identical elements",
			x:    []int{1, 2, 3},
			y:    []int{1, 2, 3},
			want: true,
		},
		{
			name: "int slice different order",
			x:    []int{1, 2, 3},
			y:    []int{3, 2, 1},
			want: true,
		},
		{
			name: "int slice different elements",
			x:    []int{1, 2, 3},
			y:    []int{1, 2, 4},
			want: false,
		},
		{
			name: "int slice different lengths",
			x:    []int{1, 2, 3},
			y:    []int{1, 2},
			want: false,
		},
		{
			name: "empty slices",
			x:    []int{},
			y:    []int{},
			want: true,
		},
		{
			name: "one empty slice",
			x:    []int{},
			y:    []int{1},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch v1 := tt.x.(type) {
			case []string:
				v2 := tt.y.([]string)
				assert.Equalf(t, tt.want, EqualElements(v1, v2), "EqualElements(%v, %v)", v1, v2)
			case []int:
				v2 := tt.y.([]int)
				assert.Equalf(t, tt.want, EqualElements(v1, v2), "EqualElements(%v, %v)", v1, v2)
			default:
				t.Errorf("Unsupported slice type in test case: %v", tt.name)
			}
		})
	}
}

func TestSimplifyHDR(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "contains HDR",
			input: []string{"HDR10", "HDR10+", "HDR"},
			want:  []string{"HDR", "HDR", "HDR"},
		},
		{
			name:  "no HDR",
			input: []string{"SDR", "DV"},
			want:  []string{"SDR", "DV"},
		},
		{
			name:  "empty slice",
			input: []string{},
			want:  []string{},
		},
		{
			name:  "mixed HDR and others",
			input: []string{"HDR10", "DV", "SDR", "HDR10+"},
			want:  []string{"HDR", "DV", "SDR", "HDR"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, SimplifyHDR(tt.input), "SimplifyHDR(%v)", tt.input)
		})
	}
}
