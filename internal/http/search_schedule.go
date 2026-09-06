// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"time"
)

// searchSchedule resets its due time when the configured interval changes.
// Runs are spaced from completion. There is no startup run or catch-up burst.
type searchSchedule struct {
	interval time.Duration
	next     time.Time
}

func (s *searchSchedule) due(now time.Time, raw string) bool {
	interval, err := time.ParseDuration(raw)
	if err != nil || interval <= 0 {
		s.interval = 0
		s.next = time.Time{}
		return false
	}
	if s.interval != interval {
		s.interval = interval
		s.next = now.Add(interval)
		return false
	}
	return !now.Before(s.next)
}

func (r *searchRunner) schedule(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var schedule searchSchedule
	schedule.due(time.Now(), r.cfg.Snapshot().Search.Interval)
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if !schedule.due(now, r.cfg.Snapshot().Search.Interval) {
				continue
			}
			if _, err := r.run(ctx, SearchRequest{}); err != nil {
				r.log.Warn().Err(err).Msg("scheduled backfill skipped")
			}
			schedule.next = time.Now().Add(schedule.interval)
		}
	}
}
