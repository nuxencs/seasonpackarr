// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"fmt"

	"github.com/nuxencs/seasonpackarr/internal/payload"

	"github.com/spf13/cobra"
)

// packCmd represents the pack command
var packCmd = &cobra.Command{
	Use:     "pack",
	Short:   "Test the pack api endpoint for a specified release",
	Example: `  seasonpackarr test pack “Series.S01.1080p.WEB-DL.H.264-RlsGrp” --client "default" --host "127.0.0.1" --port 42069 --api "your-api-key"`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			fmt.Println("Please provide a release name")
			return
		}

		rlsName = args[0]

		body, err := payload.CompilePackPayload(rlsName, clientName)
		if err != nil {
			fmt.Println(err.Error())
			return
		}

		err = payload.ExecRequest(fmt.Sprintf("http://%s:%d/api/pack", host, port), body, apiKey)
		if err != nil {
			fmt.Println(err.Error())
			return
		}
	},
}
