// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	stdhttp "net/http"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/stretchr/testify/require"
)

func TestHTTPStatusForOutcome(t *testing.T) {
	tests := []struct {
		name    string
		outcome domain.Outcome
		want    int
	}{
		{
			name:    "success",
			outcome: domain.Successful(domain.ReasonMatched),
			want:    stdhttp.StatusOK,
		},
		{
			name:    "rejection",
			outcome: domain.Rejected(domain.ReasonTorrentMismatch),
			want:    stdhttp.StatusUnprocessableEntity,
		},
		{
			name:    "request failure",
			outcome: domain.Failed(domain.ReasonMissingTorrent, domain.FaultRequest),
			want:    stdhttp.StatusBadRequest,
		},
		{
			name:    "internal failure",
			outcome: domain.Failed(domain.ReasonHardlinkFailed, domain.FaultInternal),
			want:    stdhttp.StatusInternalServerError,
		},
		{
			name:    "dependency failure",
			outcome: domain.Failed(domain.ReasonClientUnavailable, domain.FaultDependency),
			want:    stdhttp.StatusBadGateway,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, tt.outcome.Validate())
			require.Equal(t, tt.want, httpStatusForOutcome(tt.outcome))
		})
	}
}
