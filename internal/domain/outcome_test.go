// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOutcomeConstructorsEnforceCauseInvariant(t *testing.T) {
	t.Run("success has no cause", func(t *testing.T) {
		outcome := Successful(StatusSuccessfulMatch)

		require.Equal(t, OutcomeSuccess, outcome.Kind())
		require.Equal(t, StatusSuccessfulMatch, outcome.StatusCode())
		require.NoError(t, outcome.Cause())
	})

	t.Run("rejection has no cause", func(t *testing.T) {
		outcome := Rejected(StatusNoMatches)

		require.Equal(t, OutcomeRejection, outcome.Kind())
		require.Equal(t, StatusNoMatches, outcome.StatusCode())
		require.NoError(t, outcome.Cause())
	})

	t.Run("failure keeps its cause", func(t *testing.T) {
		cause := errors.New("client unavailable")
		outcome := FailedBecause(StatusGetClientError, cause)

		require.Equal(t, OutcomeFailure, outcome.Kind())
		require.Equal(t, StatusGetClientError, outcome.StatusCode())
		require.ErrorIs(t, outcome.Cause(), cause)
	})

	t.Run("failure supplies a default cause", func(t *testing.T) {
		outcome := Failed(StatusTorrentBytesError)

		require.EqualError(t, outcome.Cause(), StatusTorrentBytesError.String())
	})

	t.Run("failure with nil cause uses the status cause", func(t *testing.T) {
		outcome := FailedBecause(StatusTorrentBytesError, nil)

		require.EqualError(t, outcome.Cause(), StatusTorrentBytesError.String())
	})
}

func TestOutcomeValidationRejectsInvalidValues(t *testing.T) {
	require.Error(t, (Outcome{}).Validate())
	require.Error(t, Successful(StatusCode(999)).Validate())
	require.NoError(t, Successful(StatusSuccessfulMatch).Validate())
	require.NoError(t, Rejected(StatusNoMatches).Validate())
	require.NoError(t, Failed(StatusTorrentBytesError).Validate())
}
