// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package placement

import "syscall"

// errNotSameDevice is ERROR_NOT_SAME_DEVICE, the error of a hardlink between two volumes.
const errNotSameDevice = syscall.Errno(17)
