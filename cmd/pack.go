// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"fmt"

	"seasonpackarr/internal/payload"

	"github.com/spf13/cobra"
)

// packCmd represents the test command
var packCmd = &cobra.Command{
	Use:   "pack",
	Short: "Test the pack api endpoint for a specified release",
	Run: func(cmd *cobra.Command, args []string) {
		body, err := payload.CompileParsePayload(rlsName, nil, clientName)
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
