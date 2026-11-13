// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"fmt"

	"github.com/nuxencs/seasonpackarr/internal/payload"

	"github.com/spf13/cobra"
)

// packCmd represents the pack command
var packCmd = &cobra.Command{
	Use:   "pack",
	Short: "Test the pack API endpoint with a release or torrent file",
	Example: `  seasonpackarr test pack "Series.S01.1080p.WEB-DL.H.264-RlsGrp" --client "default" --host "127.0.0.1" --port 42069 --api "your-api-key"
  seasonpackarr test pack "/path/to/Series.S01.1080p.WEB-DL.H.264-RlsGrp.torrent" --client "default" --host "127.0.0.1" --port 42069 --api "your-api-key"`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			fmt.Println("Please provide either a release name or a .torrent file")
			return
		}

		var torrentBytes []byte
		var err error
		rlsName, torrentBytes, err = torrentInput(args[0])
		if err != nil {
			fmt.Println(err.Error())
			return
		}

		body, err := payload.CompilePack(rlsName, torrentBytes, clientName)
		if err != nil {
			fmt.Println(err.Error())
			return
		}

		err = payload.Exec(fmt.Sprintf("http://%s:%d/api/pack", host, port), body, apiKey)
		if err != nil {
			fmt.Println(err.Error())
			return
		}
	},
}
