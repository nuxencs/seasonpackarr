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
			want:  []string(nil),
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
			want:  []int(nil),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch v := tt.slice.(type) {
			case []string:
				assert.Equalf(t, tt.want, DedupeSlice(v), "Dedupe(%v)", v)
			case []int:
				assert.Equalf(t, tt.want, DedupeSlice(v), "Dedupe(%v)", v)
			default:
				t.Errorf("Unsupported slice type in test case: %v", tt.name)
			}
		})
	}
}
