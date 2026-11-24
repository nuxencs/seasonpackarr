// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package release

import (
	"slices"
	"strings"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/autobrr/rls"
)

// SeasonPack is a parsed release name that is a season pack. Only
// NewSeasonPack makes one.
type SeasonPack struct {
	release       rls.Release
	fuzzyMatching domain.FuzzyMatching
	season        SeasonKey
}

// NewSeasonPack returns the season pack of r under the fuzzy matching
// settings. It answers false when r is not a season pack: r must be of the
// series type and have no file extension.
func NewSeasonPack(r *rls.Release, fuzzyMatching domain.FuzzyMatching) (SeasonPack, bool) {
	if !r.Type.Is(rls.Series) || r.Ext != "" {
		return SeasonPack{}, false
	}
	return SeasonPack{release: *r, fuzzyMatching: fuzzyMatching, season: SeasonKeyOf(r, fuzzyMatching)}, true
}

// Season returns the season key of the season pack.
func (p SeasonPack) Season() SeasonKey { return p.season }

// Relation tells how another release relates to a season pack.
type Relation int

const (
	// OtherSeason means that the other release is of another season.
	OtherSeason Relation = iota
	// OtherReleaseVariant means the same season, and one property of the release variant differs.
	OtherReleaseVariant
	// SamePack means the same season, the same release variant, and the same episode number.
	SamePack
	// Compatible means the same season, the same release variant, and another episode number.
	Compatible
)

// Compare returns the relation of other to the season pack. For
// OtherReleaseVariant, the mismatch holds the first property that differs:
// Want is the value of the season pack and Got is the value of other, both
// before a fuzzy matching setting changes them. Other relations give a zero
// mismatch.
func (p SeasonPack) Compare(other *rls.Release) (Relation, Mismatch) {
	if SeasonKeyOf(other, p.fuzzyMatching) != p.season {
		return OtherSeason, Mismatch{}
	}
	if mismatch, ok := p.variantMismatch(other); ok {
		return OtherReleaseVariant, mismatch
	}
	if p.release.Episode == other.Episode {
		return SamePack, Mismatch{}
	}
	return Compatible, Mismatch{}
}

// variantMismatch checks the properties of the release variant in a fixed
// order and returns the first that differs.
func (p SeasonPack) variantMismatch(other *rls.Release) (Mismatch, bool) {
	pack := &p.release
	fuzzy := p.fuzzyMatching
	switch {
	case pack.Resolution != other.Resolution:
		return Mismatch{Field: MismatchResolution, Want: pack.Resolution, Got: other.Resolution}, true
	case simplifyWEB(pack.Source, fuzzy.SimplifyWebCompare) != simplifyWEB(other.Source, fuzzy.SimplifyWebCompare):
		return Mismatch{Field: MismatchSource, Want: pack.Source, Got: other.Source}, true
	case rls.MustNormalize(pack.Group) != rls.MustNormalize(other.Group):
		return Mismatch{Field: MismatchReleaseGroup, Want: pack.Group, Got: other.Group}, true
	case !equalElements(pack.Cut, other.Cut):
		return Mismatch{Field: MismatchCut, Want: pack.Cut, Got: other.Cut}, true
	case !equalElements(pack.Edition, other.Edition):
		return Mismatch{Field: MismatchEdition, Want: pack.Edition, Got: other.Edition}, true
	case !fuzzy.SkipRepackCompare && !equalElements(pack.Other, other.Other):
		return Mismatch{Field: MismatchRepackStatus, Want: pack.Other, Got: other.Other}, true
	case !equalElements(simplifyHDR(pack.HDR, fuzzy.SimplifyHdrCompare), simplifyHDR(other.HDR, fuzzy.SimplifyHdrCompare)):
		return Mismatch{Field: MismatchHDR, Want: pack.HDR, Got: other.HDR}, true
	case pack.Collection != other.Collection:
		return Mismatch{Field: MismatchStreamingService, Want: pack.Collection, Got: other.Collection}, true
	}
	return Mismatch{}, false
}

// simplifyWEB reads WEB-DL as WEB when simplify is set.
func simplifyWEB(source string, simplify bool) string {
	if simplify && source == "WEB-DL" {
		return "WEB"
	}
	return source
}

// simplifyHDR reads each HDR format as HDR when simplify is set. It returns a
// copy, so the release keeps its original formats for the mismatch.
func simplifyHDR(hdr []string, simplify bool) []string {
	if !simplify {
		return hdr
	}
	simplified := slices.Clone(hdr)
	for i := range simplified {
		if strings.Contains(simplified[i], "HDR") {
			simplified[i] = "HDR"
		}
	}
	return simplified
}

// equalElements reports whether x and y hold the same values with the same
// count of each value, in any order.
func equalElements[T comparable](x, y []T) bool {
	if len(x) != len(y) {
		return false
	}

	freqMap := make(map[T]int, len(x))
	for _, i := range x {
		freqMap[i]++
	}

	for _, i := range y {
		if freqMap[i] == 0 {
			return false
		}
		freqMap[i]--
	}

	return true
}
