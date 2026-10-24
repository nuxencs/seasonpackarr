# seasonpackarr

seasonpackarr finds TV season packs and reuses episode files that are already on disk, so only missing data downloads.

## Language

### Core flow

**Season pack**:
One torrent that holds all episodes of one TV season.
_Avoid_: Pack torrent, full season

**Candidate check**:
A check of a release name alone, done before any torrent download, that decides whether a release can be a useful season pack.
_Avoid_: Quick check, pre-check, quick candidate check

**Match**:
A check of the torrent and the episode files on disk that produces an import plan and changes nothing.
_Avoid_: Exact torrent check, parse

**Import plan**:
The result of a match: which episode files to hardlink, where, and how the pack goes into the client.
_Avoid_: Plan result

**Import**:
The step that carries out an import plan: it creates the hardlinks, then adds the season pack to the torrent client.
_Avoid_: Pack, parse

**Reusable episode**:
An episode file already on disk that an import can hardlink into the season pack folder.
_Avoid_: Existing episode, matched file

**Coverage**:
The share of distinct episode targets in a season pack that reusable episodes supply.
_Avoid_: Match percentage

**Smart mode**:
The setting that rejects a season pack when its coverage is below a threshold.

### Discovery

**Discovery**:
The ways seasonpackarr finds season packs through Prowlarr: RSS monitoring and search.
_Avoid_: Backfill

**Announce**:
A release that autobrr sends to seasonpackarr from a tracker announce.

**RSS monitoring**:
The periodic read of Prowlarr RSS feeds for new season packs.
_Avoid_: RSS backfill

**Search**:
A targeted Prowlarr search for season packs, started on demand or by an external scheduler.
_Avoid_: Backfill, manual backfill
