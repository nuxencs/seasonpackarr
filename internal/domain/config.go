// Copyright (c) 2021 - 2026, Ludvig Lundgren and the autobrr contributors.
// Code is modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

type Client struct {
	Type     string       `yaml:"type"`
	Host     string       `yaml:"host"`
	Port     int          `yaml:"port"`
	Username string       `yaml:"username"`
	Password string       `yaml:"password"`
	APIKey   string       `yaml:"apiKey"`
	Import   ImportPolicy `yaml:"import"`
}

// ImportPolicy controls how /api/parse re-imports a matched season pack back
// into the torrent client after the local episodes have been hardlinked.
//
// SavePath and Tags are client-neutral. Category, DownloadPath and
// ContentLayout are qBittorrent-only and are rejected on transmission clients
// during config validation. A correctly imported torrent is always started once
// its data has been (re)checked.
type ImportPolicy struct {
	// SavePath is the final import destination. When set it is used both as the
	// hardlink root and as the client's save/download directory. Optional for
	// qBittorrent (follows its automatic-management and manual category-path
	// preferences) and for transmission (falls back to the session download dir).
	SavePath string `yaml:"savePath"`
	// Tags are applied to the imported torrent (qBittorrent tags / transmission
	// labels).
	Tags []string `yaml:"tags"`

	// Category is the qBittorrent category to add the torrent with; also used to
	// resolve the import root when SavePath is empty. qBittorrent only.
	Category string `yaml:"category"`
	// DownloadPath is qBittorrent's temporary (incomplete) download path. It is
	// never used as the final import destination. qBittorrent only.
	DownloadPath string `yaml:"downloadPath"`
	// ContentLayout overrides the qBittorrent content layout
	// ("subfolder" | "nosubfolder" | "original"). Empty defers to the
	// qBittorrent default. qBittorrent only.
	ContentLayout string `yaml:"contentLayout"`
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
	FuzzyMatching      FuzzyMatching      `yaml:"fuzzyMatching"`
	Metadata           Metadata           `yaml:"metadata"`
	APIToken           string             `yaml:"apiToken"`
	Notifications      Notifications      `yaml:"notifications"`
}
