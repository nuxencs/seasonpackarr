// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package inventory owns the connection, the inventory, and the parsed
// torrent names of each configured client, and the scan of the source folders.
// No other package walks a source folder.
package inventory

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/release"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"

	"github.com/autobrr/rls"
)

// validity is how long a stored inventory answers reads without a client read.
const validity = 30 * time.Second

// ErrUnknownClient is the error of a client name that the config snapshot does not have.
var ErrUnknownClient = errors.New("client is not configured")

// ConnectFunc makes a torrent client adapter from a client config.
type ConnectFunc func(ctx context.Context, client *domain.Client) (torrentclient.TorrentClient, error)

// Store holds the connection, the inventory, and the parsed names of each
// client. It is safe for concurrent use.
type Store struct {
	connect ConnectFunc
	log     logger.Logger

	mu          sync.Mutex
	connections map[string]connection
	inventories map[string]*slot
	parsed      map[parsedNamesKey]parsedNames
	// folderScans holds the last finished scan of each source folder path that a scan tried. Clients with the same
	// path share it.
	folderScans map[string]folderScan
	// fullScanEnded is the end of the last scan of all folders, zero before the first one.
	fullScanEnded time.Time
	// namedFolders holds the source folders of the config of the last clean-up, and is nil before the first one. A
	// scan that ends after a clean-up stores no result for a folder that the clean-up dropped.
	namedFolders map[string]struct{}
	// scanning lets one scan run at a time. It is a channel and not a mutex, so synctest sees a waiting scan as blocked.
	scanning chan struct{}
	// reloaded tells the scan schedule about a clean-up, so it scans each new folder at once.
	reloaded chan struct{}
	// parseEpisodeName is release.ParseEpisodeName. A test can count the parses.
	parseEpisodeName func(path string) (*rls.Release, bool)
	// cleanUps counts the clean-ups. A read that overlaps a clean-up adds no
	// parsed-name key, because the clean-up can have removed that key as unused.
	cleanUps uint64
}

type connection struct {
	config  domain.Client
	adapter torrentclient.TorrentClient
}

// slot holds the inventory of one client name for one client config and
// fuzzy matching setting. A discard removes the slot from the store, so a
// read that started before the discard cannot store its result.
type slot struct {
	config domain.Client
	fuzzy  domain.FuzzyMatching
	// read lets one client read run at a time. It is a channel and not a
	// mutex, so synctest sees a caller that waits for a read as blocked.
	read chan struct{}

	// Store.mu guards these fields.
	inventory *Inventory
	expiresAt time.Time
}

// parsedNamesKey gives each inventory scope of an endpoint its own parsed
// names, so the scan of one scope cannot drop the names of another.
type parsedNamesKey struct {
	endpoint torrentclient.Endpoint
	scope    string
}

func parsedNamesKeyFor(client domain.Client) parsedNamesKey {
	return parsedNamesKey{endpoint: torrentclient.EndpointOf(client), scope: client.Inventory.Key()}
}

// parsedNames holds the parsed torrent names of one inventory scope on one
// endpoint. They survive a discard, because parsing is the costly part of a
// read. Each read replaces them with the names it saw, so they never keep the
// names of removed torrents.
type parsedNames struct {
	fuzzy domain.FuzzyMatching
	names map[string]parsedName
}

type parsedName struct {
	release *rls.Release
	// season depends on the fuzzy matching settings.
	season release.SeasonKey
}

// New returns an empty store. Production passes torrentclient.New as connect.
func New(connect ConnectFunc, log logger.Logger) *Store {
	return &Store{
		connect:     connect,
		log:         log,
		connections: make(map[string]connection),
		inventories: make(map[string]*slot),
		parsed:      make(map[parsedNamesKey]parsedNames),
		folderScans: make(map[string]folderScan),
		scanning:    make(chan struct{}, 1),
		reloaded:    make(chan struct{}, 1),

		parseEpisodeName: release.ParseEpisodeName,
	}
}

// Client is a connected client of one config snapshot.
type Client struct {
	store    *Store
	name     string
	snapshot domain.Config
	config   domain.Client
	fuzzy    domain.FuzzyMatching
	adapter  torrentclient.TorrentClient
}

// Client returns a connected handle for the client name in cfg. It uses the
// connection again when the client config did not change. A connection
// failure wraps the error of the connect function.
func (s *Store) Client(ctx context.Context, name string, cfg domain.Config) (*Client, error) {
	config, ok := cfg.Clients[name]
	if !ok || config == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnknownClient, name)
	}

	s.mu.Lock()
	cached, ok := s.connections[name]
	s.mu.Unlock()
	if !ok || !cached.config.Equal(*config) {
		// Connect without the lock, because a login can take long.
		adapter, err := s.connect(ctx, config)
		if err != nil {
			return nil, fmt.Errorf("failed to create torrent client: %w", err)
		}
		cached = connection{config: config.Clone(), adapter: adapter}
		s.mu.Lock()
		s.connections[name] = cached
		s.mu.Unlock()
	}

	return &Client{
		store:    s,
		name:     name,
		snapshot: cfg,
		config:   cached.config,
		fuzzy:    cfg.FuzzyMatching,
		adapter:  cached.adapter,
	}, nil
}

// Name returns the client name in the config.
func (c *Client) Name() string {
	return c.name
}

// Snapshot returns the config snapshot that the handle was made from, so a
// caller uses the settings that belong to this connection. Callers must not
// change it.
func (c *Client) Snapshot() domain.Config {
	return c.snapshot
}

// Adapter returns the torrent client adapter, for file reads and the import.
func (c *Client) Adapter() torrentclient.TorrentClient {
	return c.adapter
}

// Inventory returns the stored inventory of the client while it is valid, or
// reads the client. One read runs at a time for each client, and the callers
// that wait for it get its result. An error is the error of the client read,
// also when the source folders have files, because each import needs the
// client. The inventory adds the files of the last finished scan of each
// source folder of the client, and does no filesystem work for them.
func (c *Client) Inventory(ctx context.Context) (*Inventory, error) {
	for {
		current, inv := c.store.slotFor(c)
		if inv != nil {
			return c.store.withFiles(inv, c), nil
		}
		inv, replaced, err := c.store.readSlot(ctx, c, current)
		if !replaced {
			if err != nil {
				return nil, err
			}
			return c.store.withFiles(inv, c), nil
		}
	}
}

// readSlot reads the client into current, unless a read that it waited for
// stored a valid inventory. It reports replaced when a discard or a changed
// config replaced current during the wait.
func (s *Store) readSlot(ctx context.Context, c *Client, current *slot) (*Inventory, bool, error) {
	current.read <- struct{}{}
	defer func() { <-current.read }()

	if inv, stored := s.storedInventory(c.name, current); inv != nil || !stored {
		return inv, !stored, nil
	}
	inv, err := s.scan(ctx, c, current)
	return inv, false, err
}

// slotFor returns the slot of c, and its inventory while it is valid.
func (s *Store) slotFor(c *Client) (*slot, *Inventory) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.inventories[c.name]
	if current == nil || !current.config.Equal(c.config) || current.fuzzy != c.fuzzy {
		current = &slot{config: c.config, fuzzy: c.fuzzy, read: make(chan struct{}, 1)}
		s.inventories[c.name] = current
	}
	return current, current.validInventory()
}

// storedInventory reports whether the store still holds current, and its
// inventory while it is valid.
func (s *Store) storedInventory(name string, current *slot) (*Inventory, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inventories[name] != current {
		return nil, false
	}
	return current.validInventory(), true
}

// validInventory needs Store.mu.
func (sl *slot) validInventory() *Inventory {
	if sl.inventory != nil && time.Now().Before(sl.expiresAt) {
		return sl.inventory
	}
	return nil
}

func (s *Store) scan(ctx context.Context, c *Client, current *slot) (*Inventory, error) {
	s.mu.Lock()
	cleanUps := s.cleanUps
	s.mu.Unlock()
	torrents, err := c.adapter.GetTorrents(ctx)
	if err != nil {
		return nil, err
	}
	// The validity starts at the client answer, not after the parse.
	expiresAt := time.Now().Add(validity)

	key := parsedNamesKeyFor(c.config)
	previous := s.previousNames(key, c.fuzzy)
	names := make(map[string]parsedName, len(torrents))
	inv := &Inventory{seasons: make(map[release.SeasonKey][]Entry), torrents: len(torrents)}
	for _, t := range torrents {
		// Cross-seeds repeat a name in the same scan.
		parsed, ok := names[t.Name]
		for _, earlier := range previous {
			if ok {
				break
			}
			parsed, ok = earlier[t.Name]
		}
		if !ok {
			r := new(rls.ParseString(t.Name))
			parsed = parsedName{release: r, season: release.SeasonKeyOf(r, c.fuzzy)}
		}
		names[t.Name] = parsed
		inv.seasons[parsed.season] = append(inv.seasons[parsed.season], Entry{Torrent: t, Release: parsed.release})
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Parsed names stay valid after a discard, so store them then too.
	if _, ok := s.parsed[key]; ok || s.cleanUps == cleanUps {
		s.parsed[key] = parsedNames{fuzzy: c.fuzzy, names: names}
	}
	// A discard during the read removed current from the store, so no later call sees this inventory.
	current.inventory = inv
	current.expiresAt = expiresAt
	return inv, nil
}

// previousNames returns the parsed names of key first, then those of the
// other scopes of its endpoint, so a name in two scopes is parsed once.
func (s *Store) previousNames(key parsedNamesKey, fuzzy domain.FuzzyMatching) []map[string]parsedName {
	s.mu.Lock()
	defer s.mu.Unlock()
	var previous []map[string]parsedName
	if own, ok := s.parsed[key]; ok && own.fuzzy == fuzzy {
		previous = append(previous, own.names)
	}
	for other, names := range s.parsed {
		if other != key && other.endpoint == key.endpoint && names.fuzzy == fuzzy {
			previous = append(previous, names.names)
		}
	}
	return previous
}

// DiscardClient drops the inventory of the client name. A read in progress
// returns its result to its caller, but does not store it.
func (s *Store) DiscardClient(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inventories, name)
}

// DiscardEndpoint drops the inventory of each client on endpoint, because an
// import through one client changes what the others see. A read in progress
// returns its result to its caller, but does not store it.
func (s *Store) DiscardEndpoint(endpoint torrentclient.Endpoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	maps.DeleteFunc(s.inventories, func(_ string, sl *slot) bool {
		return torrentclient.EndpointOf(sl.config) == endpoint
	})
}

// Entry is one entry of an inventory: a torrent of the client, or a file of a source folder.
type Entry struct {
	// Torrent is the zero value for a file entry.
	Torrent torrentclient.Torrent
	// File is the path of a file of a source folder, and is empty for a torrent entry. A file entry has no hash.
	File string
	// SourceFolder is the source folder whose scan listed File, and is empty for a torrent entry. A hardlink source
	// must resolve below it.
	SourceFolder string
	// Release is shared by reads and by the clients of one endpoint or source folder. Callers must not change it.
	Release *rls.Release
}

// IsFile reports whether the entry is a file of a source folder.
func (e Entry) IsFile() bool {
	return e.File != ""
}

// Name returns the torrent name, or the path of a file entry, for the log lines.
func (e Entry) Name() string {
	if e.IsFile() {
		return e.File
	}
	return e.Torrent.Name
}

// Inventory is a read-only view of the torrents of one client read and the files of the last scans of its source
// folders, grouped by season. Its methods yield copies of the entries and no slice, so one caller cannot change what
// another sees, and a lookup copies no group.
type Inventory struct {
	seasons  map[release.SeasonKey][]Entry
	torrents int // the torrents in the read, for the startup log
	// folderSeasons holds the file entries of each source folder of the client, grouped by season.
	folderSeasons []map[release.SeasonKey][]Entry
}

// SameSeason yields the entries of the season key, the torrent entries before the file entries. A file in two source
// folders of the client, one below the other, comes once, with the folder that comes first in the config. The caller
// must build the key with the fuzzy matching settings of the Client that gave the inventory (Client.Snapshot), because
// the read grouped the entries with those settings.
func (inv *Inventory) SameSeason(key release.SeasonKey) iter.Seq[Entry] {
	// The lookup is inside the iterator, so SameSeason stays small enough to
	// inline and a range over it allocates nothing.
	return func(yield func(Entry) bool) {
		for _, e := range inv.seasons[key] {
			if !yield(e) {
				return
			}
		}
		if len(inv.folderSeasons) == 0 {
			return
		}
		inv.sameSeasonFiles(key, yield)
	}
}

func (inv *Inventory) sameSeasonFiles(key release.SeasonKey, yield func(Entry) bool) {
	var seen map[string]struct{}
	if len(inv.folderSeasons) > 1 {
		seen = make(map[string]struct{})
	}
	for _, folder := range inv.folderSeasons {
		for _, e := range folder[key] {
			if seen != nil {
				if _, ok := seen[e.File]; ok {
					continue
				}
				seen[e.File] = struct{}{}
			}
			if !yield(e) {
				return
			}
		}
	}
}

// SeasonGroups yields the entries of each season, in no fixed order. Each group is the SameSeason of its key: the
// torrent entries before the file entries, and a file once. A caller can range over one group more than once.
func (inv *Inventory) SeasonGroups() iter.Seq[iter.Seq[Entry]] {
	return func(yield func(iter.Seq[Entry]) bool) {
		// With no file, a group is its torrent slice, so the range needs no key set and no merge.
		if len(inv.folderSeasons) == 0 {
			for _, group := range inv.seasons {
				if !yield(slices.Values(group)) {
					return
				}
			}
			return
		}
		yielded := make(map[release.SeasonKey]struct{}, len(inv.seasons))
		for key := range inv.seasons {
			yielded[key] = struct{}{}
			if !yield(inv.SameSeason(key)) {
				return
			}
		}
		// A season with files and no torrent is in no torrent group.
		for _, folder := range inv.folderSeasons {
			for key := range folder {
				if _, ok := yielded[key]; ok {
					continue
				}
				yielded[key] = struct{}{}
				if !yield(inv.SameSeason(key)) {
					return
				}
			}
		}
	}
}
