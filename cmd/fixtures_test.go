// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func isolateCLI(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "SEASONPACKARR__") {
			t.Setenv(key, "")
		}
	}
	t.Setenv("SEASONPACKARR__DISABLE_CONFIG_FILE", "true")
	t.Chdir(t.TempDir())
}

func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := execute(t.Context(), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}
