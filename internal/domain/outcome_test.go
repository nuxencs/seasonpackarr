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
		outcome := Successful(ReasonMatched)

		require.Equal(t, OutcomeSuccess, outcome.Kind())
		require.Equal(t, ReasonMatched, outcome.Reason())
		require.Empty(t, outcome.FaultClass())
		require.NoError(t, outcome.Cause())
	})

	t.Run("rejection has no cause", func(t *testing.T) {
		outcome := Rejected(ReasonNoMatches)

		require.Equal(t, OutcomeRejection, outcome.Kind())
		require.Equal(t, ReasonNoMatches, outcome.Reason())
		require.Empty(t, outcome.FaultClass())
		require.NoError(t, outcome.Cause())
	})

	t.Run("failure keeps its cause", func(t *testing.T) {
		cause := errors.New("client unavailable")
		outcome := FailedBecause(ReasonClientUnavailable, FaultDependency, cause)

		require.Equal(t, OutcomeFailure, outcome.Kind())
		require.Equal(t, ReasonClientUnavailable, outcome.Reason())
		require.Equal(t, FaultDependency, outcome.FaultClass())
		require.ErrorIs(t, outcome.Cause(), cause)
	})

	t.Run("failure supplies a default cause", func(t *testing.T) {
		outcome := Failed(ReasonMissingTorrent, FaultRequest)

		require.EqualError(t, outcome.Cause(), ReasonMissingTorrent.Message())
	})

	t.Run("failure with nil cause uses the reason cause", func(t *testing.T) {
		outcome := FailedBecause(ReasonMissingTorrent, FaultRequest, nil)

		require.EqualError(t, outcome.Cause(), ReasonMissingTorrent.Message())
	})
}

func TestOutcomeValidationRejectsInvalidValues(t *testing.T) {
	require.Error(t, (Outcome{}).Validate())
	require.Error(t, Successful(Reason("unknown")).Validate())
	require.Error(t, Failed(ReasonMissingTorrent, FaultClass("unknown")).Validate())
	require.NoError(t, Successful(ReasonMatched).Validate())
	require.NoError(t, Rejected(ReasonNoMatches).Validate())
	require.NoError(t, Failed(ReasonMissingTorrent, FaultRequest).Validate())
}
