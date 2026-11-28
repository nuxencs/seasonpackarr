// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package placement

import "syscall"

// errNotSameDevice is the error of a hardlink between two filesystems.
const errNotSameDevice = syscall.EXDEV
