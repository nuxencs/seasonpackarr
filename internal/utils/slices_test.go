// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_DedupeSlice(t *testing.T) {
	tests := []struct {
		name  string
		slice interface{}
		want  interface{}
	}{
		{
			name:  "string_slice_some_duplicates",
			slice: []string{"string_1", "string_2", "string_3", "string_2", "string_1"},
			want:  []string{"string_1", "string_2", "string_3"},
		},
		{
			name:  "string_slice_all_duplicates",
			slice: []string{"string_1", "string_1", "string_1", "string_1", "string_1"},
			want:  []string{"string_1"},
		},
		{
			name:  "string_slice_no_duplicates",
			slice: []string{"string_1", "string_2", "string_3"},
			want:  []string{"string_1", "string_2", "string_3"},
		},
		{
			name:  "string_slice_empty",
			slice: []string{},
			want:  []string{},
		},
		{
			name:  "int_slice_some_duplicates",
			slice: []int{1, 2, 3, 2, 1},
			want:  []int{1, 2, 3},
		},
		{
			name:  "int_slice_all_duplicates",
			slice: []int{1, 1, 1, 1, 1},
			want:  []int{1},
		},
		{
			name:  "int_slice_no_duplicates",
			slice: []int{1, 2, 3},
			want:  []int{1, 2, 3},
		},
		{
			name:  "int_slice_empty",
			slice: []int{},
			want:  []int{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch v := tt.slice.(type) {
			case []string:
				assert.ElementsMatchf(t, tt.want, DedupeSlice(v), "Dedupe(%v)", v)
			case []int:
				assert.ElementsMatchf(t, tt.want, DedupeSlice(v), "Dedupe(%v)", v)
			default:
				t.Errorf("Unsupported slice type in test case: %v", tt.name)
			}
		})
	}
}

func Test_EqualElements(t *testing.T) {
	tests := []struct {
		name string
		x    interface{}
		y    interface{}
		want bool
	}{
		{
			name: "string_slice_identical_elements",
			x:    []string{"a", "b", "c"},
			y:    []string{"a", "b", "c"},
			want: true,
		},
		{
			name: "string_slice_different_order",
			x:    []string{"a", "b", "c"},
			y:    []string{"c", "b", "a"},
			want: true,
		},
		{
			name: "string_slice_different_elements",
			x:    []string{"a", "b", "c"},
			y:    []string{"a", "b", "d"},
			want: false,
		},
		{
			name: "string_slice_different_lengths",
			x:    []string{"a", "b", "c"},
			y:    []string{"a", "b"},
			want: false,
		},
		{
			name: "int_slice_identical_elements",
			x:    []int{1, 2, 3},
			y:    []int{1, 2, 3},
			want: true,
		},
		{
			name: "int_slice_different_order",
			x:    []int{1, 2, 3},
			y:    []int{3, 2, 1},
			want: true,
		},
		{
			name: "int_slice_different_elements",
			x:    []int{1, 2, 3},
			y:    []int{1, 2, 4},
			want: false,
		},
		{
			name: "int_slice_different_lengths",
			x:    []int{1, 2, 3},
			y:    []int{1, 2},
			want: false,
		},
		{
			name: "empty_slices",
			x:    []int{},
			y:    []int{},
			want: true,
		},
		{
			name: "one_empty_slice",
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
