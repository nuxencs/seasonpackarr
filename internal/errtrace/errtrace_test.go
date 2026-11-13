// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package errtrace

import (
	"context"
	"errors"
	"testing"

	zerologpkgerrors "github.com/rs/zerolog/pkgerrors"
	"github.com/stretchr/testify/require"
)

func TestWithStack_PreservesErrorIdentityAndAddsOneStack(t *testing.T) {
	sentinel := errors.New("boom")

	traced := WithStack(sentinel)
	require.ErrorIs(t, traced, sentinel)
	stack, ok := errors.AsType[stackTracer](traced)
	require.True(t, ok)
	require.NotEmpty(t, stack.StackTrace())
	require.NotNil(t, zerologpkgerrors.MarshalStack(traced))
	require.Same(t, traced, WithStack(traced))
}

func TestWithStack_LeavesExpectedCancellationUnchanged(t *testing.T) {
	require.ErrorIs(t, WithStack(context.Canceled), context.Canceled)
	require.Equal(t, context.Canceled, WithStack(context.Canceled))
	require.Equal(t, context.DeadlineExceeded, WithStack(context.DeadlineExceeded))
	require.NoError(t, WithStack(nil))
}
