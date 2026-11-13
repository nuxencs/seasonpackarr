// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"fmt"

	"github.com/nuxencs/seasonpackarr/internal/payload"

	"github.com/spf13/cobra"
)

// parseCmd represents the parse command
var parseCmd = &cobra.Command{
	Use:   "parse",
	Short: "Test the parse api endpoint for a specified release",
	Example: `  seasonpackarr test parse "Series.S01.1080p.WEB-DL.H.264-RlsGrp" --client "default" --host "127.0.0.1" --port 42069 --api "your-api-key"
  seasonpackarr test parse "/path/to/Series.S01.1080p.WEB-DL.H.264-RlsGrp.torrent" --client "default" --host "127.0.0.1" --port 42069 --api "your-api-key"`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			fmt.Println("Please provide either a release name or a .torrent file to parse")
			return
		}

		var torrentBytes []byte
		var err error
		rlsName, torrentBytes, err = torrentInput(args[0])
		if err != nil {
			fmt.Println(err.Error())
			return
		}

		body, err := payload.CompileParse(rlsName, torrentBytes, clientName)
		if err != nil {
			fmt.Println(err.Error())
			return
		}

		err = payload.Exec(fmt.Sprintf("http://%s:%d/api/parse", host, port), body, apiKey)
		if err != nil {
			fmt.Println(err.Error())
			return
		}
	},
}
