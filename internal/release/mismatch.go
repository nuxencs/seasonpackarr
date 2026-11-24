// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package release

// MismatchField names one property that differs. The values are the field
// names of the operator log lines.
type MismatchField string

const (
	MismatchResolution       MismatchField = "resolution"
	MismatchSource           MismatchField = "source"
	MismatchReleaseGroup     MismatchField = "release_group"
	MismatchCut              MismatchField = "cut"
	MismatchEdition          MismatchField = "edition"
	MismatchRepackStatus     MismatchField = "repack_status"
	MismatchHDR              MismatchField = "hdr"
	MismatchStreamingService MismatchField = "streaming_service"
	MismatchSize             MismatchField = "size"
	MismatchContainer        MismatchField = "container"
)

// Mismatch reports one property that differs. Want is the required value and
// Got is the value that was found. The caller that makes the mismatch defines
// which side is which.
type Mismatch struct {
	Field MismatchField
	Want  any
	Got   any
}
