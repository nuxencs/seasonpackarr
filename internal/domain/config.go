// Copyright (c) 2021 - 2023, Ludvig Lundgren and the autobrr contributors.
// Code is slightly modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

type Config struct {
	Version       string
	ConfigPath    string
	Host          string `toml:"host"`
	Port          int    `toml:"port"`
	PreImportPath string `toml:"preImportPath"`
	LogPath       string `toml:"logPath"`
	LogLevel      string `toml:"logLevel"`
	LogMaxSize    int    `toml:"logMaxSize"`
	LogMaxBackups int    `toml:"logMaxBackups"`
}
