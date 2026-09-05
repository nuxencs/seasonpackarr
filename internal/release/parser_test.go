// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package release

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/autobrr/rls"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestCheckCandidates_SiloWebTitle(t *testing.T) {
	const packName = "Silo S03 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-FLUX"
	const episodeName = "Silo.S03E03.A.Dark.Web.1080p.ATVP.WEB-DL.DDP5.1.Atmos.H.264-FLUX.mkv"
	const episodeSize = int64(3_961_454_446)

	pack := rls.ParseString(packName)
	episode := rls.ParseString(episodeName)
	result := CheckCandidates(pack, episode, domain.FuzzyMatching{})
	require.Equal(t, domain.StatusSuccessfulMatch, result.StatusCode, "%+v", result)

	target := filepath.Join(packName, episodeName)
	path, info := MatchEpToSeasonPackEp(episodeName, episodeSize, target, episodeSize)
	require.Equal(t, target, path)
	require.Equal(t, domain.CompareInfo{}, info)

	otherSource := rls.ParseString(strings.Replace(episodeName, "WEB-DL", "WEB", 1))
	result = CheckCandidates(pack, otherSource, domain.FuzzyMatching{})
	require.Equal(t, domain.StatusSourceMismatch, result.StatusCode)
}
