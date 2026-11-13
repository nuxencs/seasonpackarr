// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"fmt"

	"github.com/nuxencs/seasonpackarr/internal/payload"

	"github.com/spf13/cobra"
)

var candidateCmd = &cobra.Command{
	Use:     "candidate",
	Short:   "Test the candidate API endpoint for a specified release",
	Example: `  seasonpackarr test candidate "Series.S01.1080p.WEB-DL.H.264-RlsGrp" --client "default" --host "127.0.0.1" --port 42069 --api "your-api-key"`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			fmt.Println("Please provide a release name")
			return
		}

		body, err := payload.CompileCandidate(args[0], clientName)
		if err != nil {
			fmt.Println(err.Error())
			return
		}

		if err = payload.Exec(fmt.Sprintf("http://%s:%d/api/candidate", host, port), body, apiKey); err != nil {
			fmt.Println(err.Error())
		}
	},
}
