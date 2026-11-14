// Copyright (c) 2021 - 2026, Ludvig Lundgren and the autobrr contributors.
// Code is modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

import (
	"slices"
	"strings"
)

type Client struct {
	Type     string       `yaml:"type"`
	Host     string       `yaml:"host"`
	Port     int          `yaml:"port"`
	Username string       `yaml:"username"`
	Password string       `yaml:"password"`
	APIKey   string       `yaml:"apiKey"`
	Import   ImportPolicy `yaml:"import"`
	// Inventory limits the torrents that seasonpackarr reads from the client. qBittorrent only.
	Inventory InventoryScope `yaml:"inventory"`
}

// InventoryScope selects the torrents of the inventory. A torrent is in the
// scope when it has an include category or an include tag, or when the scope
// has no includes, and it has no exclude category and no exclude tag. An
// exclude always wins. An empty scope selects every torrent. The scope never
// applies to the import.
type InventoryScope struct {
	IncludeCategories []string `yaml:"includeCategories"`
	IncludeTags       []string `yaml:"includeTags"`
	ExcludeCategories []string `yaml:"excludeCategories"`
	ExcludeTags       []string `yaml:"excludeTags"`
}

// Clone copies the lists, so the copy shares no memory with s.
func (s InventoryScope) Clone() InventoryScope {
	return InventoryScope{
		IncludeCategories: slices.Clone(s.IncludeCategories),
		IncludeTags:       slices.Clone(s.IncludeTags),
		ExcludeCategories: slices.Clone(s.ExcludeCategories),
		ExcludeTags:       slices.Clone(s.ExcludeTags),
	}
}

// Equal reports whether both scopes have the same lists. Nil and empty lists are equal.
func (s InventoryScope) Equal(other InventoryScope) bool {
	return slices.Equal(s.IncludeCategories, other.IncludeCategories) &&
		slices.Equal(s.IncludeTags, other.IncludeTags) &&
		slices.Equal(s.ExcludeCategories, other.ExcludeCategories) &&
		slices.Equal(s.ExcludeTags, other.ExcludeTags)
}

// Key returns the same text for equal scopes, for use as a map key.
func (s InventoryScope) Key() string {
	// A NUL byte cannot be part of a qBittorrent category or tag.
	return strings.Join([]string{
		strings.Join(s.IncludeCategories, "\x00"),
		strings.Join(s.IncludeTags, "\x00"),
		strings.Join(s.ExcludeCategories, "\x00"),
		strings.Join(s.ExcludeTags, "\x00"),
	}, "\x01")
}

// IsSet reports whether the scope limits the inventory.
func (s InventoryScope) IsSet() bool {
	return s.HasIncludes() || len(s.ExcludeCategories) > 0 || len(s.ExcludeTags) > 0
}

// HasIncludes reports whether the scope selects only the torrents with an include category or tag.
func (s InventoryScope) HasIncludes() bool {
	return len(s.IncludeCategories) > 0 || len(s.IncludeTags) > 0
}

// IncludesTag reports whether the tag is an include tag.
func (s InventoryScope) IncludesTag(tag string) bool {
	return slices.Contains(s.IncludeTags, tag)
}

// ExcludesCategory reports whether the category is an exclude category. It
// matches the exact name only, so it does not exclude subcategories.
func (s InventoryScope) ExcludesCategory(category string) bool {
	return slices.Contains(s.ExcludeCategories, category)
}

// ExcludesTag reports whether the tag is an exclude tag.
func (s InventoryScope) ExcludesTag(tag string) bool {
	return slices.Contains(s.ExcludeTags, tag)
}

// Selects reports whether a torrent with the category and tags is surely in
// the scope. It matches exact names. qBittorrent can also read the "/"
// subcategories of an include category: always from 5.2, before only when subcategories are on.
func (s InventoryScope) Selects(category string, tags []string) bool {
	if s.ExcludesCategory(category) || slices.ContainsFunc(tags, s.ExcludesTag) {
		return false
	}
	return !s.HasIncludes() || slices.Contains(s.IncludeCategories, category) || slices.ContainsFunc(tags, s.IncludesTag)
}

// ImportPolicy controls how /api/import re-imports a matched season pack back
// into the torrent client after the local episodes have been hardlinked.
//
// SavePath is shared by all clients. Tags apply to qBittorrent and Transmission.
// Deluge accepts at most one tag as its Label plugin label. Category,
// DownloadPath and ContentLayout are qBittorrent-only. A correctly imported
// torrent is always started once its data has been checked.
type ImportPolicy struct {
	// SavePath is the final import destination. When set it is used both as the
	// hardlink root and as the client's save/download directory. Optional for
	// qBittorrent (follows its automatic-management and manual category-path
	// preferences) and for Transmission (falls back to the session download
	// dir). Deluge requires an explicit path.
	SavePath string `yaml:"savePath"`
	// Tags are applied to the imported torrent (qBittorrent tags / Transmission
	// labels). Deluge accepts at most one entry as its Label plugin label.
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
	// Shoutrrr holds one Shoutrrr URL for each notification destination other than the Discord webhook.
	Shoutrrr []string `yaml:"shoutrrr"`
}

// Search configures Prowlarr search and opt-in RSS monitoring.
type Search struct {
	IndexerIDs      []int  `yaml:"indexerIDs"`
	ProwlarrURL     string `yaml:"prowlarrURL"`
	APIKey          string `yaml:"apiKey"`
	RSSInterval     string `yaml:"rssInterval"`
	RequestInterval string `yaml:"requestInterval"`
}

type Config struct {
	// The runtime sets these fields. The "-" tags keep config.yaml from setting
	// them, so the config load rejects them as unknown keys.
	Version            string             `yaml:"-" koanf:"-"`
	ConfigPath         string             `yaml:"-" koanf:"-"`
	DisableConfigFile  bool               `yaml:"-" koanf:"-"`
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
	APIToken           string             `yaml:"apiToken"`
	Notifications      Notifications      `yaml:"notifications"`
	Search             Search             `yaml:"search"`
}
