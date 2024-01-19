// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package utils

import "golang.org/x/exp/slices"

func DedupeSlice[T comparable](s []T) []T {
	inResult := make(map[T]bool)
	var result []T
	for _, str := range s {
		if _, ok := inResult[str]; !ok {
			inResult[str] = true
			result = append(result, str)
		}
	}
	return result
}

func CompareStringSlices(x, y []string) bool {
	slices.Sort(x)
	slices.Sort(y)

	if slices.Equal(x, y) {
		return true
	}
	return false
}
