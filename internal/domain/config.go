// Copyright (c) 2021 - 2023, Ludvig Lundgren and the autobrr contributors.
// Code is slightly modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

type Client struct {
	Host          string `yaml:"host"`
	Port          int    `yaml:"port"`
	Username      string `yaml:"username"`
	Password      string `yaml:"password"`
	PreImportPath string `yaml:"preImportPath"`
}

type Config struct {
	Version       string
	ConfigPath    string
	Host          string             `yaml:"host"`
	Port          int                `yaml:"port"`
	Clients       map[string]*Client `yaml:"clients"`
	LogPath       string             `yaml:"logPath"`
	LogLevel      string             `yaml:"logLevel"`
	LogMaxSize    int                `yaml:"logMaxSize"`
	LogMaxBackups int                `yaml:"logMaxBackups"`
	APIToken      string             `yaml:"apiToken"`
}
