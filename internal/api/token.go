// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"github.com/google/uuid"
)

func GenerateToken() string {
	return uuid.New().String()
}
