// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The save paths do not exist: Validate does no file access, and the config load checks them.
func TestValidate_AppliesClientTypeRules(t *testing.T) {
	t.Parallel()

	type validateCase struct {
		name    string
		client  domain.Client
		wantErr string
	}
	tests := []validateCase{
		{
			name:    "qbittorrent requires category or savePath",
			client:  domain.Client{Type: "qbittorrent"},
			wantErr: "import.category or import.savePath",
		},
		{
			name:   "qbittorrent with category is valid",
			client: domain.Client{Type: "qbittorrent", Import: domain.ImportPolicy{Category: "tv-hd"}},
		},
		{
			name:   "qbittorrent with savePath is valid",
			client: domain.Client{Type: "qbittorrent", Import: domain.ImportPolicy{SavePath: "/data/tv"}},
		},
		{
			name:   "empty type is qbittorrent",
			client: domain.Client{Import: domain.ImportPolicy{Category: "tv-hd"}},
		},
		{
			name:    "empty type gets the qbittorrent rules",
			client:  domain.Client{},
			wantErr: "import.category or import.savePath",
		},
		{
			name:   "qbittorrent accepts each content layout",
			client: domain.Client{Import: domain.ImportPolicy{Category: "tv-hd", ContentLayout: "nosubfolder"}},
		},
		{
			name:    "qbittorrent rejects an invalid content layout",
			client:  domain.Client{Type: "qbittorrent", Import: domain.ImportPolicy{SavePath: "/data/tv", ContentLayout: "flat"}},
			wantErr: "import.contentLayout",
		},
		{
			// The adapter trims the layout, but the config rule is strict.
			name:    "qbittorrent rejects a padded content layout",
			client:  domain.Client{Type: "qbittorrent", Import: domain.ImportPolicy{Category: "tv-hd", ContentLayout: " original"}},
			wantErr: "import.contentLayout",
		},
		{
			name:    "transmission rejects import.category",
			client:  domain.Client{Type: "transmission", Import: domain.ImportPolicy{Category: "tv-hd"}},
			wantErr: "qBittorrent only",
		},
		{
			name:    "transmission rejects import.downloadPath",
			client:  domain.Client{Type: "transmission", Import: domain.ImportPolicy{DownloadPath: "/data/incomplete"}},
			wantErr: "qBittorrent only",
		},
		{
			name:    "transmission rejects import.contentLayout",
			client:  domain.Client{Type: "transmission", Import: domain.ImportPolicy{ContentLayout: "original"}},
			wantErr: "qBittorrent only",
		},
		{
			name:   "transmission with savePath is valid",
			client: domain.Client{Type: "transmission", Import: domain.ImportPolicy{SavePath: "/data/tv"}},
		},
		{
			name:   "transmission without any policy is valid",
			client: domain.Client{Type: "transmission"},
		},
		{
			name:    "deluge requires savePath",
			client:  domain.Client{Type: "deluge"},
			wantErr: "import.savePath",
		},
		{
			name:   "deluge with savePath is valid",
			client: domain.Client{Type: "deluge", Import: domain.ImportPolicy{SavePath: "/data/tv"}},
		},
		{
			name:   "deluge accepts one label",
			client: domain.Client{Type: "deluge-v1", Import: domain.ImportPolicy{SavePath: "/data/tv", Tags: []string{"seasonpackarr"}}},
		},
		{
			name:    "deluge rejects multiple labels",
			client:  domain.Client{Type: "deluge-v2", Import: domain.ImportPolicy{SavePath: "/data/tv", Tags: []string{"seasonpackarr", "tv"}}},
			wantErr: "at most one",
		},
		{
			name:    "deluge rejects invalid label characters",
			client:  domain.Client{Type: "deluge", Import: domain.ImportPolicy{SavePath: "/data/tv", Tags: []string{"tv shows"}}},
			wantErr: "import label",
		},
		{
			name:    "deluge rejects a blank label",
			client:  domain.Client{Type: "deluge", Import: domain.ImportPolicy{SavePath: "/data/tv", Tags: []string{" "}}},
			wantErr: "import label",
		},
		{
			name:    "deluge rejects qbittorrent-only import keys",
			client:  domain.Client{Type: "deluge-v2", Import: domain.ImportPolicy{SavePath: "/data/tv", Category: "tv-hd"}},
			wantErr: "qBittorrent only",
		},
		{
			name:    "deluge rejects apiKey",
			client:  domain.Client{Type: "deluge", APIKey: "secret", Import: domain.ImportPolicy{SavePath: "/data/tv"}},
			wantErr: "apiKey",
		},
		{
			name:    "unknown type",
			client:  domain.Client{Type: "notarealclient", Import: domain.ImportPolicy{SavePath: "/data/tv"}},
			wantErr: `type "notarealclient" is invalid`,
		},
		{
			name: "qbittorrent accepts an inventory scope",
			client: domain.Client{
				Type:      "qbittorrent",
				Import:    domain.ImportPolicy{Category: "tv-hd"},
				Inventory: domain.InventoryScope{IncludeCategories: []string{"tv-hd"}, IncludeTags: []string{"keep"}},
			},
		},
	}
	for _, clientType := range []string{"transmission", "deluge", "deluge-v1", "deluge-v2"} {
		tests = append(tests, validateCase{
			name: clientType + " rejects an inventory scope",
			client: domain.Client{
				Type:      clientType,
				Import:    domain.ImportPolicy{SavePath: "/data/tv"},
				Inventory: domain.InventoryScope{ExcludeTags: []string{"noreuse"}},
			},
			wantErr: "must not set inventory",
		})
	}
	for name, scope := range map[string]domain.InventoryScope{
		"empty category entry is rejected":         {IncludeCategories: []string{"tv-hd", ""}},
		"blank tag entry is rejected":              {IncludeTags: []string{" "}},
		"empty exclude category entry is rejected": {ExcludeCategories: []string{""}},
		"empty exclude tag entry is rejected":      {ExcludeTags: []string{"noreuse", ""}},
		// qBittorrent compares the exact name, so " keep " would match no torrent.
		"include tag with spaces is rejected":       {IncludeTags: []string{" keep "}},
		"exclude category with a space is rejected": {ExcludeCategories: []string{"movies "}},
	} {
		tests = append(tests, validateCase{
			name:    name,
			client:  domain.Client{Type: "qbittorrent", Import: domain.ImportPolicy{Category: "tv-hd"}, Inventory: scope},
			wantErr: "inventory list entry",
		})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := Validate(tt.client)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestInventorySelectsImports(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		client domain.Client
		want   bool
	}{
		{
			name:   "no scope",
			client: domain.Client{Import: domain.ImportPolicy{Category: "tv-hd"}},
			want:   true,
		},
		{
			name: "scope includes the padded import category",
			client: domain.Client{
				Import:    domain.ImportPolicy{Category: " tv-hd "},
				Inventory: domain.InventoryScope{IncludeCategories: []string{"tv-hd"}},
			},
			want: true,
		},
		{
			name: "scope includes a padded import tag",
			client: domain.Client{
				Import:    domain.ImportPolicy{Category: "tv-hd", Tags: []string{" keep "}},
				Inventory: domain.InventoryScope{IncludeTags: []string{"keep"}},
			},
			want: true,
		},
		{
			name: "scope misses the import category and tags",
			client: domain.Client{
				Import:    domain.ImportPolicy{Category: "tv-hd", Tags: []string{"seasonpackarr"}},
				Inventory: domain.InventoryScope{IncludeCategories: []string{"tv-hd-completed"}},
			},
		},
		// Validate rejects an empty scope entry, but only this scope shows that a blank tag is not sent.
		{
			name: "blank import tag is dropped",
			client: domain.Client{
				Import:    domain.ImportPolicy{Category: "tv-hd", Tags: []string{" "}},
				Inventory: domain.InventoryScope{ExcludeTags: []string{""}},
			},
			want: true,
		},
		{
			name: "excludes only keep the import category",
			client: domain.Client{
				Import:    domain.ImportPolicy{Category: "tv-hd", Tags: []string{"seasonpackarr"}},
				Inventory: domain.InventoryScope{ExcludeCategories: []string{"movies"}},
			},
			want: true,
		},
		{
			name: "excludes only drop the padded import tag",
			client: domain.Client{
				Import:    domain.ImportPolicy{Category: "tv-hd", Tags: []string{" seasonpackarr "}},
				Inventory: domain.InventoryScope{ExcludeTags: []string{"seasonpackarr"}},
			},
		},
		{
			name: "client type without an inventory scope",
			client: domain.Client{
				Type:      "transmission",
				Import:    domain.ImportPolicy{Tags: []string{"seasonpackarr"}},
				Inventory: domain.InventoryScope{IncludeTags: []string{"tv"}},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, InventorySelectsImports(tt.client))
		})
	}
}
