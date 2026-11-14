// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package notification

import "github.com/nuxencs/seasonpackarr/internal/domain"

// fakeConfig is a config provider with a fixed config.
type fakeConfig struct{ config domain.Config }

func (f fakeConfig) Snapshot() domain.Config { return f.config }
