// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"context"
	"slices"

	"github.com/nuxencs/seasonpackarr/internal/domain"
)

// clientType is one entry of clientTypes. Each rule that depends on the
// client type belongs in its entry, so a new client type has one place.
type clientType struct {
	// name is the canonical spelling of the type in the config.
	name string
	// aliases are the other spellings that select this type.
	aliases    []string
	newAdapter constructor
}

// constructor connects to the client of a config and returns its adapter.
type constructor func(context.Context, *domain.Client) (TorrentClient, error)

var clientTypes = []clientType{
	// An empty type is qBittorrent, the default client type.
	{name: "qbittorrent", aliases: []string{""}, newAdapter: asConstructor(newQbitClient)},
	{name: "transmission", newAdapter: asConstructor(newTransmissionClient)},
	{name: "deluge-v1", newAdapter: delugeConstructor(delugeV1)},
	{name: "deluge-v2", aliases: []string{"deluge"}, newAdapter: delugeConstructor(delugeV2)},
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
