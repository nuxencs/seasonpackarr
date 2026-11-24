// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/nuxencs/seasonpackarr/internal/domain"
)

// clientType is one entry of clientTypes. Each rule that depends on the
// client type belongs in its entry, so a new client type has one place.
type clientType struct {
	// name is the canonical spelling of the type in the config.
	name string
	// aliases are the other spellings that select this type.
	aliases []string
	// importCategoryAndTags returns the category and tags that the add gives a new pack.
	// Nil means that the adapter applies no inventory scope, which validate checks.
	importCategoryAndTags func(domain.ImportPolicy) (category string, tags []string)
	// address builds the connect address from host and port. Validate, the adapter constructor, and EndpointOf use it.
	address func(domain.Client) (*url.URL, error)
	// validate checks the rules of this type. typeName is the canonical name, for the message.
	validate   func(typeName string, client domain.Client) error
	newAdapter constructor
}

// constructor connects to the client of a config and returns its adapter.
type constructor func(context.Context, *domain.Client) (TorrentClient, error)

var clientTypes = []clientType{
	// An empty type is qBittorrent, the default client type.
	{
		name: "qbittorrent", aliases: []string{""}, importCategoryAndTags: qbitImportCategoryAndTags,
		address: httpAddress, validate: validateQbitConfig, newAdapter: asConstructor(newQbitClient),
	},
	{
		name: "transmission", address: transmissionAddress, validate: rejectQbitOnlyImportKeys,
		newAdapter: asConstructor(newTransmissionClient),
	},
	{name: "deluge-v1", address: delugeAddress, validate: validateDelugeConfig, newAdapter: delugeConstructor(delugeV1)},
	{
		name: "deluge-v2", aliases: []string{"deluge"}, address: delugeAddress, validate: validateDelugeConfig,
		newAdapter: delugeConstructor(delugeV2),
	},
}

// Validate reports whether client is a valid config for its client type. It
// does no file access. Its messages name the config keys but not the client,
// so the caller adds the client name.
func Validate(client domain.Client) error {
	_, err := validate(client)
	return err
}

// validate is Validate, and it also returns the entry of the client type for New.
func validate(client domain.Client) (clientType, error) {
	entry, ok := lookupClientType(client.Type)
	if !ok {
		return clientType{}, fmt.Errorf("type %q is invalid - use %s", client.Type, clientTypeSpellings())
	}
	if _, err := entry.address(client); err != nil {
		return clientType{}, err
	}
	// go-qbittorrent drops an empty filter value, which would read every torrent,
	// and qBittorrent compares exact names, so " tv" would match no torrent.
	scope := client.Inventory
	for _, value := range slices.Concat(scope.IncludeCategories, scope.IncludeTags, scope.ExcludeCategories, scope.ExcludeTags) {
		if value == "" || strings.TrimSpace(value) != value {
			return clientType{}, fmt.Errorf("inventory list entry %q is empty or starts or ends with a space", value)
		}
	}
	if scope.IsSet() && entry.importCategoryAndTags == nil {
		return clientType{}, fmt.Errorf("type %q must not set inventory - the inventory scope is qBittorrent only", entry.name)
	}
	if err := entry.validate(entry.name, client); err != nil {
		return clientType{}, err
	}
	return entry, nil
}

// InventorySelectsImports reports whether the inventory scope of client
// surely selects the packs that its import adds. A client with no scope set,
// a client type that applies no inventory scope, and an unknown type answer true.
func InventorySelectsImports(client domain.Client) bool {
	entry, ok := lookupClientType(client.Type)
	if !ok || entry.importCategoryAndTags == nil || !client.Inventory.IsSet() {
		return true
	}
	return client.Inventory.Selects(entry.importCategoryAndTags(client.Import))
}

// clientTypeSpellings lists each type spelling of the config for a message.
func clientTypeSpellings() string {
	var spellings []string
	for _, entry := range clientTypes {
		spellings = append(spellings, strconv.Quote(entry.name))
		for _, alias := range entry.aliases {
			if alias != "" {
				spellings = append(spellings, strconv.Quote(alias))
			}
		}
	}
	return orList(spellings)
}

// orList joins values for a message, such as "a, b or c".
func orList(values []string) string {
	if len(values) < 2 {
		return strings.Join(values, "")
	}
	last := len(values) - 1
	return strings.Join(values[:last], ", ") + " or " + values[last]
}

// rejectQbitOnlyImportKeys is the import rule of each client type that is not qBittorrent.
func rejectQbitOnlyImportKeys(typeName string, client domain.Client) error {
	imp := client.Import
	if imp.Category != "" || imp.DownloadPath != "" || imp.ContentLayout != "" {
		return fmt.Errorf("type %q must not set import.category, import.downloadPath or import.contentLayout - those are qBittorrent only", typeName)
	}
	return nil
}

// lookupClientType returns the entry that the type spelling of a client
// config selects.
func lookupClientType(spelling string) (clientType, bool) {
	index := slices.IndexFunc(clientTypes, func(entry clientType) bool {
		return entry.name == spelling || slices.Contains(entry.aliases, spelling)
	})
	if index < 0 {
		return clientType{}, false
	}
	return clientTypes[index], true
}

// asConstructor returns a nil TorrentClient on error, so a caller never gets a
// non-nil interface that holds a nil adapter.
func asConstructor[A TorrentClient](newAdapter func(context.Context, *domain.Client) (A, error)) constructor {
	return func(ctx context.Context, client *domain.Client) (TorrentClient, error) {
		adapter, err := newAdapter(ctx, client)
		if err != nil {
			return nil, err
		}
		return adapter, nil
	}
}

func delugeConstructor(version delugeVersion) constructor {
	return asConstructor(func(ctx context.Context, client *domain.Client) (*delugeClient, error) {
		return newDelugeClient(ctx, client, version)
	})
}
