// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

func importStageNames(report ImportReport) []ImportStage {
	stages := make([]ImportStage, len(report.Stages))
	for index, stage := range report.Stages {
		stages[index] = stage.Stage
	}
	return stages
}
