// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package utils

import (
	"slices"
	"strings"
)

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
	if len(x) != len(y) {
		return false
	}

	sortedX := slices.Clone(x)
	sortedY := slices.Clone(y)

	slices.Sort(sortedX)
	slices.Sort(sortedY)

	return slices.Equal(sortedX, sortedY)
}

func SimplifyHDRSlice(hdrSlice []string) []string {
	if len(hdrSlice) == 0 {
		return hdrSlice
	}

	for i, v := range hdrSlice {
		if strings.Contains(v, "HDR") {
			hdrSlice[i] = "HDR"
		}
	}

	return hdrSlice
}
