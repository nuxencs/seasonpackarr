// Copyright (c) 2021 - 2025, Ludvig Lundgren and the autobrr contributors.
// Code is modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

type Client struct {
	Type          string `yaml:"type"`
	Host          string `yaml:"host"`
	Port          int    `yaml:"port"`
	Username      string `yaml:"username"`
	Password      string `yaml:"password"`
	APIKey        string `yaml:"apiKey"`
	PreImportPath string `yaml:"preImportPath"`
}

type FuzzyMatching struct {
	SkipRepackCompare  bool `yaml:"skipRepackCompare"`
	SimplifyHdrCompare bool `yaml:"simplifyHdrCompare"`
	SimplifyWebCompare bool `yaml:"simplifyWebCompare"`
	SkipYearCompare    bool `yaml:"skipYearCompare"`
}

type Notifications struct {
	NotificationLevel []string `yaml:"notificationLevel"`
	Discord           string   `yaml:"discord"`
	// Notifiarr string `yaml:"notifiarr"`
	// Shoutrrr  string `yaml:"shoutrrr"`
}

type Metadata struct {
	TVDBAPIKey string `yaml:"tvdbAPIKey"`
	TVDBPIN    string `yaml:"tvdbPIN"`
	// SonarrHost   string `yaml:"sonarrHost"`
	// SonarrPort   int    `yaml:"sonarrPort"`
	// SonarrAPIKey string `yaml:"sonarrAPIKey"`
}

type Config struct {
	Version            string
	ConfigPath         string
	DisableConfigFile  bool
	Host               string             `yaml:"host"`
	Port               int                `yaml:"port"`
	Clients            map[string]*Client `yaml:"clients"`
	LogPath            string             `yaml:"logPath"`
	LogLevel           string             `yaml:"logLevel"`
	LogMaxSize         int                `yaml:"logMaxSize"`
	LogMaxBackups      int                `yaml:"logMaxBackups"`
	SmartMode          bool               `yaml:"smartMode"`
	SmartModeThreshold float32            `yaml:"smartModeThreshold"`
	ParseTorrentFile   bool               `yaml:"parseTorrentFile"`
	FuzzyMatching      FuzzyMatching      `yaml:"fuzzyMatching"`
	Metadata           Metadata           `yaml:"metadata"`
	APIToken           string             `yaml:"apiToken"`
	Notifications      Notifications      `yaml:"notifications"`
}
