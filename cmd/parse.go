// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"fmt"

	"seasonpackarr/internal/payload"
	"seasonpackarr/internal/torrents"

	"github.com/spf13/cobra"
)

// parseCmd represents the test command
var parseCmd = &cobra.Command{
	Use:   "parse",
	Short: "Test the parse api endpoint for a specified release",
	Run: func(cmd *cobra.Command, args []string) {
		torrentBytes, err := torrents.TorrentFromRls(rlsName, 5)
		if err != nil {
			fmt.Println(err.Error())
			return
		}

		body, err := payload.CompileParsePayload(rlsName, torrentBytes, clientName)
		if err != nil {
			fmt.Println(err.Error())
			return
		}

		err = payload.ExecRequest(fmt.Sprintf("http://%s:%d/api/parse", host, port), body, apiKey)
		if err != nil {
			fmt.Println(err.Error())
			return
		}
	},
}
