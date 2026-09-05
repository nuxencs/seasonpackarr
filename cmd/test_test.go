// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTestCommand_RejectsRemovedOperations(t *testing.T) {
	stdout, stderr := rootCmd.OutOrStdout(), rootCmd.ErrOrStderr()
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	t.Cleanup(func() {
		rootCmd.SetOut(stdout)
		rootCmd.SetErr(stderr)
		rootCmd.SetArgs(nil)
	})

	for _, operation := range []string{"pack", "parse"} {
		t.Run(operation, func(t *testing.T) {
			rootCmd.SetArgs([]string{"test", operation})
			err := rootCmd.ExecuteContext(t.Context())
			require.ErrorContains(t, err, "unknown command")
			require.ErrorContains(t, err, operation)
		})
	}
}
