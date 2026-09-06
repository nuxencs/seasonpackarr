// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/prowlarr"
)

func validateSearch(search domain.Search) error {
	seen := make(map[int]bool)
	for _, id := range search.IndexerIDs {
		if id <= 0 || seen[id] {
			return fmt.Errorf("search.indexerIDs must contain unique positive integers")
		}
		seen[id] = true
	}
	interval, err := time.ParseDuration(search.Interval)
	if err != nil || interval < 0 || (interval > 0 && interval < time.Hour) {
		return fmt.Errorf("search.interval must be 0s (disabled) or at least 1h")
	}
	spacing, err := time.ParseDuration(search.RequestInterval)
	if err != nil || spacing < 10*time.Second {
		return fmt.Errorf("search.requestInterval must be at least 10s")
	}
	if search.ProwlarrURL == "" && search.APIKey == "" && interval == 0 {
		return nil
	}
	if _, err := prowlarr.New(search.ProwlarrURL, search.APIKey, spacing); err != nil {
		return fmt.Errorf("search: %w", err)
	}
	return nil
}

func parseSearchIndexerIDs(value string) []int {
	parts := strings.Split(value, ",")
	ids := make([]int, len(parts))
	for i, part := range parts {
		id, err := strconv.Atoi(strings.TrimSpace(part))
		if err == nil {
			ids[i] = id
		}
		// Invalid entries remain zero so validation rejects the whole configuration.
	}
	return ids
}
