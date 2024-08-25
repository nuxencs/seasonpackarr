// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package utils

import (
	"strings"
)

func DedupeSlice[T comparable](s []T) []T {
	resultSet := make(map[T]struct{})
	for _, i := range s {
		resultSet[i] = struct{}{}
	}

	result := make([]T, 0, len(resultSet))
	for str := range resultSet {
		result = append(result, str)
	}

	return result
}

func EqualElements[T comparable](x, y []T) bool {
	if len(x) != len(y) {
		return false
	}

	freqMap := make(map[T]int)
	for _, i := range x {
		freqMap[i]++
	}

	for _, i := range y {
		if freqMap[i] == 0 {
			return false
		}
		freqMap[i]--
	}

	return true
}

func SimplifyHDRSlice(hdrSlice []string) []string {
	for i := range hdrSlice {
		if strings.Contains(hdrSlice[i], "HDR") {
			hdrSlice[i] = "HDR"
		}
	}

	return hdrSlice
}
