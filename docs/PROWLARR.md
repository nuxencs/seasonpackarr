# Prowlarr discovery

Discovery finds season packs through Prowlarr in two ways:

- **RSS monitoring** reads the Prowlarr RSS feeds on a fixed interval. It is off
  by default.
- **Search** runs a targeted Prowlarr search when you start it. Use it to find
  older packs or to fill gaps after downtime.

Both start from the episode torrents in your configured clients and use the
same match and import steps as autobrr. seasonpackarr does not search for shows
that are not in a client, and it does not download single episodes.

Discovery and autobrr work independently. Use either one, or both. For
automatic searches without autobrr or RSS, run `seasonpackarr search` from an
external scheduler such as cron. Prowlarr is not required for autobrr, and
autobrr is not required for discovery.

seasonpackarr imports one pack per release variant. An equivalent pack already
in the client blocks another pack from any tracker, even when the torrent hash
is different. Cross-tracker duplicates belong to other applications.

## Configure Prowlarr

Add this block to `config.yaml`:

```yaml
search:
  indexerIDs: [] # All eligible indexers; use [2, 5] to restrict tracker access.
  prowlarrURL: "http://prowlarr:9696"
  apiKey: "your-prowlarr-api-key"
  rssInterval: "0s" # Set to "15m" to enable RSS imports. Minimum: "10m".
  requestInterval: "10s" # Minimum: 10s.
```

`prowlarrURL` is the base URL, including the URL base when used. Do not append
`/api/v1`. `apiKey` is Prowlarr's API key. The service does not need a download
client configured in Prowlarr.

`indexerIDs` applies to RSS polls and searches. An empty list selects all
enabled Prowlarr torrent indexers that support the requested mode: `supportsRss`
for RSS, `supportsSearch` for search. RSS-only indexers are supported.
A nonempty list restricts searches to those Prowlarr IDs. IDs must be unique
positive integers. Missing, disabled, or unsupported selections produce failures;
the run never falls back to unselected trackers. Eligible selected indexers still
run. Read IDs from Prowlarr's `GET /api/v1/indexer` response.

`rssInterval: "0s"` disables RSS monitoring. Set a positive Go duration,
for example `"15m"`, to enable RSS imports. Positive intervals must be at least
`"10m"`. The first poll starts after the interval. Later intervals start when the
previous poll finishes. There is no startup run. Searches run only when you start them.

`requestInterval` sets the minimum time between requests to Prowlarr, including
indexer discovery, RSS polls, searches, and torrent retrieval across
runs. Default and minimum: `"10s"`.
A failed feed request skips that indexer for the rest of the run. Temporary failures
also create a cooldown that applies to searches and RSS polls:

| Failure | Fallback cooldown |
| --- | --- |
| HTTP 429 | 10 minutes |
| HTTP 408 or 5xx, connection failure, timeout, or interrupted response read | 10 minutes |

For these HTTP errors, a valid `Retry-After` sets the deadline directly. Both
seconds and HTTP dates are supported. Use the ten-minute fallback only when the
header is missing or invalid. Zero seconds or a past HTTP date adds no delay to
later runs. Normal request spacing still applies, and the affected indexer is
skipped for the remainder of the current run. There are no automatic request
retries or exponential backoff. Cancellation does not create a cooldown.

A cooldown stops both searches and torrent retrieval for the affected indexer.
Other indexers can continue. A failure during indexer discovery blocks all
requests to that Prowlarr connection until its cooldown expires.

Cooldowns are stored in SQLite and survive service restarts. A Prowlarr URL or
API key change clears them on the next discovery run. Normal request spacing
still applies, but its last-request timestamp is not persisted.

These limits are project choices, not tracker-specific guarantees. A poll can
require multiple requests. Select an interval that fits the configured trackers.

All search settings reload without a restart. The scheduler detects an interval
change within one second while idle and resets its next run time. An active run
keeps the complete config snapshot with which it started. Its client connections,
matching rules, and import policies stay consistent until that run finishes.

Environment overrides:

- `SEASONPACKARR__SEARCH_PROWLARR_URL`
- `SEASONPACKARR__SEARCH_API_KEY`
- `SEASONPACKARR__SEARCH_INDEXER_IDS` (comma-separated IDs, for example `2,5`)
- `SEASONPACKARR__SEARCH_RSS_INTERVAL`
- `SEASONPACKARR__SEARCH_REQUEST_INTERVAL`

## RSS monitoring

Each poll reads recent releases once per eligible indexer, regardless of how many
series or seasons are in the client. It sends `t=search` without a title, year,
season, or external ID. TV category `5000` is used when advertised. This reads a
feed through Prowlarr; it does not send a grab to Prowlarr's download clients.

The first poll reads one page, up to 100 items or the indexer's lower page limit.
Later polls start at the newest page and can page back until a result overlaps
with the previous poll. Paging stops after ten pages, a short or repeated page,
or on a non-paginating indexer. A missing overlap reports a possible gap and
recommends a search. After a reported gap, the newest page becomes
the next checkpoint. An HTTP failure preserves the previous checkpoint for the
next attempt. When a client inventory is unavailable, the poll uses isolated
feed state so it cannot consume entries on behalf of that client. RSS cannot
guarantee historical coverage.

Results must parse as season packs for an uncovered series/season in the client.
Year compatibility is checked against the episodes before metadata retrieval. The same variant, source-file, exact-coverage, and duplicate checks listed
below then apply. RSS respects indexer priority and feed order. One indexer failure
does not prevent other indexers from running. No eligible episode groups means no
Prowlarr requests.

The process retains relevant pack listings for seven days, bounded to 1024 entries
and 4 MiB of titles and links. Oldest entries are evicted first. Each poll checks
retained packs against current inventory, local files, and matching settings,
even if a pack has left the feed. A pack rejected for low coverage can pass once
more episodes finish. Valid cached torrent bytes avoid another metadata download.
There is no permanent "seen means rejected" decision.

Feed checkpoints and retained listings are stored in SQLite and survive restarts.
A checkpoint and its retained listings are saved together before result evaluation.
Changing the Prowlarr URL or API key resets them on the next discovery run. Disabled
or unselected indexers are removed from feed state after successful RSS indexer
selection. Retained links are refreshed
when the same result appears again, without extending the retention deadline.
Use a search for releases outside the feed and retention window.

## Search

Start the service, then preview a search:

```sh
seasonpackarr search --dry-run
```

This dry run queries the selected trackers and runs candidate checks. It does
not request per-torrent file details or download torrent metadata. Passing results
have status `candidate`, with unknown exact coverage. Alternatives can appear
across trackers because no exact selection has been made. Tracker search limits
still apply.

To run a match for each result, add `--verify`:

```sh
seasonpackarr search --dry-run --verify
```

This preview checks local source files, downloads or reuses torrent metadata,
and applies [smart mode](CONFIGURATION.md#smart-mode). It reports `would_import` for accepted results.
Neither preview mode creates hardlinks, adds torrents, or sends notifications.
`--verify` requires `--dry-run`. An import always runs a match first.

To import the accepted packs, run the search without flags:

```sh
seasonpackarr search
```

RSS monitoring runs only on its configured interval and always evaluates imports
for all clients. The CLI and API run searches across the configured searchable
indexers. The preview flags apply only to searches.

All three modes scan all configured clients unless `SEASONPACKARR__CLIENT`
selects one. Add `--client default` to select one client.
Use `--url http://127.0.0.1:42069` to set the seasonpackarr base
URL. The CLI reads connection settings from local config and environment. Add
`--config <directory>` for a custom config directory. The `--api` token belongs
to seasonpackarr, not Prowlarr. See the [CLI guide](CLI.md#connection-settings).

### Search output

The CLI prints a readable summary. Add `--json` to get the complete API report
with scan counts, logical search group count, search request
count, completed torrent downloads, metadata cache hits, per-result outcomes,
and operation failures. `requests` counts targeted feed requests, excluding
indexer discovery and torrent downloads. Automatic RSS logs include `rss: true`.
`coveredEpisodeTorrents` counts episode torrent entries
excluded because a compatible pack already exists. `episodeTorrents` includes
these entries; `groups` counts only groups that still need a search. Counts are
per client and include duplicate episode torrents, not distinct episodes.
The CLI exits with a failure code
if a tracker, client, or result failed. Normal match rejections are not command
failures. Logs contain the same outcomes for automatic runs.

### Cancel a search

A search request stays connected until it finishes. Cancel the command to cancel
its run. A reverse proxy must allow long requests. The service cancels automatic
work during shutdown. A cancelled import can have already created hardlinks or
added a stopped torrent; inspect the client before retrying.

## How seasonpackarr selects packs

1. Read fresh torrent summaries from each selected client.
2. Identify episode torrents with a parsed title, positive season, and positive
   episode number. Ignore movies, season packs, specials, date-based releases,
   and names that do not provide those identifiers. Scan and episode counts show
   how much inventory was eligible.
3. Before creating queries, exclude episode torrents whose release variant
   already has a compatible season pack in the same client. Apply the existing
   release and fuzzy-matching rules, including year comparison. A pack for one
   variant does not suppress another variant. This check uses summaries only.
   Group the remaining episodes by normalized series title and season. Different
   or missing years share one query. Keep parsed years in the local release
   checks, subject to `skipYearCompare`. Reuse a query across clients while
   keeping their source files and import policies separate. A
   covered client does not suppress a search needed by an independent client.
   If no groups remain, do not request Prowlarr indexer discovery or searches.
4. For a search, read enabled, searchable torrent indexers and apply
   `search.indexerIDs`. Try lower numeric indexer priorities first, with indexer
   ID as the tie-breaker.
5. Use a TV title-and-season query when the indexer advertises those parameters.
   Otherwise use a title and `Sxx` text query. Omit the parsed release year from
   both query forms so trackers can return packs whose names omit it. Keep the
   year in local compatibility checks, subject to `skipYearCompare`.
   Request TV category `5000` when
   advertised; omit the category filter for trackers without a TV category.
   Indexers without either query capability are reported as failures.
6. Check results in feed order against existing release rules. Reject an
   equivalent pack already in the client or already selected for that endpoint.
   Require at least one compatible episode torrent. Search-only dry run stops
   here and reports `candidate` with unknown coverage.
7. For exact preview and import, read the compatible episode file details.
   Require at least one accessible regular file with a positive size that equals
   the client's declared size. Exclude unavailable or size-mismatched files from
   the plan. A client file-detail request failure stops that result before
   metadata retrieval. File size alone does not prove completion or valid pieces.
8. Reuse valid cached metadata for this indexer and result identity, or download
   the torrent through Prowlarr. Build an exact plan using available source files.
   The file list, sizes, and distinct valid episode targets determine coverage;
   release titles and total torrent size cannot establish it.
9. Apply current smart-mode settings. With smart mode disabled, at least one
   exact reusable file is still required. Accept the first passing pack per
   release variant. Exact preview records a proposed selection. Import creates
   hardlinks and adds the torrent through the existing client adapter. The
   client checks the data and starts the torrent.

Release compatibility includes the existing resolution, source, release group,
cut, edition, repack, HDR, and streaming-service rules. Fuzzy matching options
remain in effect. There is no new quality-ranking system.

Configurations that point to the same client type, host, and port share duplicate
protection, even when they use different import categories. Use consistent host
URLs for aliases of the same client. Independent clients receive separate imports.

For example, NTb episodes and an NTb pack suppress that variant. NTb episodes
and a FLUX pack still cause a search. Existing packs are checked again on every
run, so removing a pack makes its episode variant eligible again.

After an import attempt, that variant is not tried again during the same run.
This prevents a client verification or resume failure from triggering another
tracker copy. A later run checks the client again. If the pack was added but is
stopped, recover it in the torrent client.

## Metadata reuse

SQLite keeps valid torrent metadata for up to seven days, limited to 64 MiB
and 1024 entries. The least recently used entries are removed when needed.
Restarting the service preserves the cache. Changing the Prowlarr URL or API key
clears it on the next discovery run.
Keys include the indexer ID, result GUID (or download link when no GUID exists),
and release title. Different tracker results do not share cached bytes.

Rejected exact matches are cached too. Later runs recheck current inventory,
local files, matching settings, and coverage against those bytes. New episodes or
a lower threshold can make a previous rejection pass without another torrent
download. Dry runs without `--verify` do not read or populate this cache. Invalid torrent
responses are not cached. Cache expiry or eviction can cause another download.
A tracker that replaces metadata under the same result identity can remain stale
until expiry or a connection change. The cache assumes stable tracker result identities.

## Discovery state

seasonpackarr keeps discovery state in its SQLite database: RSS checkpoints,
retained listings, cooldowns, and cached torrent metadata. See
[Application data](INSTALL.md#application-data) for the database location,
backups, and startup errors. Cached metadata and retained links can contain
tracker credentials, so protect database backups as you protect `config.yaml`.

A database failure during discovery stops further work in that run. It appears
in the failure report and in the service logs. Imports that finished before the
failure stay in place. Fix the storage problem before you run discovery again.

## API

`POST /api/search` runs a search with the same options as the CLI. See
[Search](API.md#search) in the API reference for the request, status codes, and
report fields.

## Limits

- Requests to Prowlarr time out after two minutes and accept at most 32 MiB.
- Searches request up to 100 results per page, reduced to the indexer's advertised
  maximum. Each group/indexer pair has a ten-page budget. A repeated full page or
  exhausted budget produces an explicit incomplete-results failure.
- Non-paginating indexers receive one request. Prowlarr may itself hide a tracker
  failure as an empty feed. Check Prowlarr logs when empty results are unexpected.
- Downloads must use the corresponding Prowlarr torrent proxy. Direct tracker
  links, magnet links, and redirects are rejected. Disable tracker redirect and
  prefer-magnet settings in Prowlarr when they prevent torrent-byte retrieval.
- No external series database or TVDB/TMDB lookup is used. Ambiguous names are
  not expanded through guesses. Discovery does not search for shows absent from
  the client and does not remove episode torrents or files.
- Only one RSS or search run executes at a time. Imports to the same client endpoint
  are serialized with webhook imports. Matching stays free of client mutations.

The Prowlarr API facts behind these limits are in the
[Prowlarr API audit](research/prowlarr-backfill-api.md).
