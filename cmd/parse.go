// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"seasonpackarr/internal/payload"
	"seasonpackarr/internal/torrents"
	"seasonpackarr/pkg/errors"

	"github.com/spf13/cobra"
)

// parseCmd represents the parse command
var parseCmd = &cobra.Command{
	Use:   "parse",
	Short: "Test the parse api endpoint for a specified release",
	Example: `  seasonpackarr test parse “Series.S01.1080p.WEB-DL.H.264-RlsGrp” --client "default" --host "127.0.0.1" --port 42069 --api "your-api-key"
  seasonpackarr test parse “/path/to/Series.S01.1080p.WEB-DL.H.264-RlsGrp.torrent” --client "default" --host "127.0.0.1" --port 42069 --api "your-api-key"`,
	Run: func(cmd *cobra.Command, args []string) {
		var torrentBytes []byte
		var body io.Reader
		var err error

		if len(args) == 0 {
			fmt.Println("Please provide either a release name or a .torrent file to parse")
			return
		}

		torrentFile = args[0]
		// trim .torrent extension and remove full path for rlsName
		rlsName = strings.TrimSuffix(filepath.Base(torrentFile), ".torrent")

		if filepath.Ext(torrentFile) != ".torrent" {
			torrentBytes, err = torrents.TorrentFromRls(rlsName, 5)
			if err != nil {
				fmt.Println(err.Error())
				return
			}
		} else {
			if len(torrentFile) == 0 {
				fmt.Println("The path of the torrent file can't be empty")
				return
			}

			if _, err := os.Stat(torrentFile); errors.Is(err, fs.ErrNotExist) {
				fmt.Println("The specified torrent file doesn't exist", err.Error())
				return
			}

			torrentBytes, err = os.ReadFile(torrentFile)
			if err != nil {
				fmt.Println(err.Error())
				return
			}
		}

		body, err = payload.CompileParsePayload(rlsName, torrentBytes, clientName)
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
