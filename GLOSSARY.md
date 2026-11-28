# seasonpackarr

seasonpackarr finds TV season packs and reuses episode files that are already on disk, so only missing data downloads.

## Language

### Core flow

**Season pack**:
One torrent that holds all episodes of one TV season.
_Avoid_: Pack torrent, full season

**Season**:
One season of one show, as release names identify it: the show title, the season number, and the year. A name with no year and a name with a year are different seasons. With `fuzzyMatching.skipYearCompare`, the year is not part of the season.
_Avoid_: Comparable title, title (for this meaning)

**Release variant**:
The properties of a release that must be equal before a season pack can reuse an episode: resolution, source, release group, cut, edition, repack status, HDR, and streaming service. The fuzzy matching settings relax some of them. An episode is compatible with a season pack when both have the same season and the same release variant.
_Avoid_: Flavor, version

**Candidate check**:
A check of a release name alone against the inventory, with no torrent bytes, that decides whether a release can be a useful season pack. The match and the import do it again.
_Avoid_: Quick check, pre-check, quick candidate check, candidate gate

**Match**:
A check of the torrent and the episode files on disk that accepts or rejects a season pack and changes nothing. An accepted match produces an import plan.
_Avoid_: Exact torrent check, parse

**Import plan**:
The result of an accepted match: which episode files to hardlink, where, and how the pack goes into the client.
_Avoid_: Plan result

**Import**:
The step that carries out an import plan: it creates the hardlinks, then adds the season pack to the torrent client.
_Avoid_: Pack, parse

**Reusable episode**:
An episode file already on disk that an import can hardlink into the pack folder.
_Avoid_: Existing episode, matched file

**Import root**:
The folder of the torrent client below which an import puts a season pack.
_Avoid_: Import folder, import destination

**Pack folder**:
The folder below the import root that has the name of the season pack and holds its files. A client layout without a subfolder has no pack folder.
_Avoid_: Season pack folder

**Hardlink target**:
The path below the import root where one file of the season pack must be.

**Coverage**:
The share of distinct episode targets in a season pack that reusable episodes supply.
_Avoid_: Match percentage

**Smart mode**:
The setting that rejects a season pack when its coverage is below a threshold.

**Inventory**:
The torrents of a client and the episode files in its source folders, which seasonpackarr reads for the candidate check, the match, and discovery.
_Avoid_: Torrent list, client torrents

**Inventory scope**:
The client setting that limits the torrents of the inventory: include categories and tags select torrents, and exclude categories and tags drop them. An empty scope is every torrent. It does not limit source folders.
_Avoid_: Inventory filter

**Source folder**:
A folder of one client whose episode files are in the inventory, although no torrent of the client holds them. seasonpackarr reads the file names as they are, whichever tool put the files there.
_Avoid_: Library, library path, local path

**Client type**:
The torrent client software and protocol version of a client: qBittorrent, Transmission, Deluge 1, or Deluge 2. An alias is a second spelling of one client type.
_Avoid_: Client kind

**Endpoint**:
The torrent client service that a client connects to: the client type and the address that the adapter connects to, in each spelling of the config. Clients with the same endpoint share one service, so an import through one of them changes what the others see.
_Avoid_: Alias, client alias

### Discovery

**Discovery**:
The ways seasonpackarr finds season packs through Prowlarr: RSS monitoring and search.
_Avoid_: Backfill

**Announce**:
A release that autobrr sends to seasonpackarr from a tracker announce.

**Filter**:
The autobrr filter that sends announces for one client to the candidate check, the match, and the import.
_Avoid_: autobrr config, filter config

**RSS monitoring**:
The periodic read of Prowlarr RSS feeds for new season packs.
_Avoid_: RSS backfill

**Search**:
A targeted Prowlarr search for season packs, started on demand or by an external scheduler.
_Avoid_: Backfill, manual backfill

**Search group**:
The episodes of one show title and one season number in the inventories of a search, of each year and each client. A search sends one query for it to each indexer.
_Avoid_: Season group, title group

**Query gap**:
The minimum time between two queries to one indexer, from a search or from RSS monitoring. It does not apply to torrent downloads.
_Avoid_: Rate limit, delay

**Wait time**:
The time after a search queried one indexer for a search group and a client, during which no search sends that query again.
_Avoid_: Cooldown (for this meaning), exclude time

**Search time**:
The stored time at which an import run got the answer of a query of one search group to one indexer, for one client. The wait time starts at the search time.
_Avoid_: Last search, timestamp

**Discovery run**:
One search or one RSS poll, from its start to its report. Only one is active at a time.
_Avoid_: Job, scan

**Indexer**:
One tracker that Prowlarr exposes for search or RSS monitoring.

**Listing**:
One release that an indexer offers, from a search or an RSS feed.
_Avoid_: Result, RSS candidate

**Cooldown**:
The time after a failure during which seasonpackarr sends no request to one indexer, or to Prowlarr as a whole.
_Avoid_: Pause, backoff

**Checkpoint**:
The newest listings of the last RSS poll of one indexer. The next poll reads back to them.

**Retained listing**:
A listing that RSS monitoring keeps, so later polls can check its coverage again.
_Avoid_: RSS candidate

**Torrent cache**:
The stored torrent bytes of listings, so a later discovery run does not download them again.
_Avoid_: Metadata, metadata cache

**Outcome**:
The evaluation of one listing in the report of a discovery run, with its status: `candidate`, `would_import`, `imported`, `rejected`, or `failed`.

### Notifications

**Notification**:
A message to the operator about an accepted season pack, a rejected season pack, or an error.
_Avoid_: Alert, webhook

**Notification destination**:
One place that receives notifications, such as one Discord webhook or one Gotify server.
_Avoid_: Sender, provider, target
