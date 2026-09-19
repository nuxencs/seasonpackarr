# Prowlarr Discovery

## Purpose

Discover season packs through opt-in Prowlarr RSS monitoring.
Use manual targeted searches for historical backfill or gaps after downtime.
Both modes start from episode torrents in configured clients and use the existing
matching, hardlink, and import flow.

Prowlarr RSS and autobrr can each provide automatic discovery independently, or
run together. Enable at least one for automation, or use an external scheduler
such as cron to run targeted Prowlarr searches. Without an automation source,
start searches on demand. Prowlarr is not required for the autobrr workflow;
autobrr is not required for RSS or targeted searches.

The service accepts one pack per release variant. An equivalent pack already in
that client blocks another pack from any tracker, even when its torrent hash is
different. Cross-tracker duplication belongs to other applications.

## Configuration

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

`indexerIDs` applies to RSS polls and targeted searches. An empty list selects all
enabled Prowlarr torrent indexers that support the requested mode: `supportsRss`
for RSS, `supportsSearch` for targeted search. RSS-only indexers are supported.
A nonempty list restricts searches to those Prowlarr IDs. IDs must be unique
positive integers. Missing, disabled, or unsupported selections produce failures;
the run never falls back to unselected trackers. Eligible selected indexers still
run. Read IDs from Prowlarr's `GET /api/v1/indexer` response.

`rssInterval: "0s"` disables RSS monitoring. Set a positive Go duration,
for example `"15m"`, to enable RSS imports. Positive intervals must be at least
`"10m"`. The first poll starts after the interval. Later intervals start when the
previous poll finishes. There is no startup run. Targeted searches are manual only.

`requestInterval` sets the minimum time between requests to Prowlarr, including
indexer discovery, RSS polls, targeted searches, and torrent retrieval across
runs. Default and minimum: `"10s"`.
A failed feed request skips that indexer for the rest of the run. Temporary failures
also create a cooldown that applies to manual and scheduled runs:

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

## RSS Monitoring

Each poll reads recent releases once per eligible indexer, regardless of how many
series or seasons are in the client. It sends `t=search` without a title, year,
season, or external ID. TV category `5000` is used when advertised. This reads a
feed through Prowlarr; it does not send a grab to Prowlarr's download clients.

The first poll reads one page, up to 100 items or the indexer's lower page limit.
Later polls start at the newest page and can page back until a result overlaps
with the previous poll. Paging stops after ten pages, a short or repeated page,
or on a non-paginating indexer. A missing overlap reports a possible gap and
recommends a manual targeted search. After a reported gap, the newest page becomes
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
Use manual backfill for releases outside the feed and retention window.

## Manual Runs

Start the service, then preview a targeted historical search:

```sh
seasonpackarr search --dry-run --api "your-seasonpackarr-api-token"
```

This dry run queries selected trackers and applies release-name checks. It does
not request per-torrent file details or download torrent metadata. Passing results
have status `candidate`, with unknown exact coverage. Alternatives can appear
across trackers because no exact selection has been made. Tracker search limits
still apply.

To check exact reuse, explicitly enable verification:

```sh
seasonpackarr search --dry-run --verify --api "your-seasonpackarr-api-token"
```

Exact preview checks local source files, retrieves or reuses torrent metadata,
and applies smart-mode coverage. It reports `would_import` for accepted results.
Neither preview mode creates hardlinks, adds torrents, or sends notifications.
`--verify` requires `--dry-run`. Imports always use exact verification.

Import accepted packs:

```sh
seasonpackarr search --api "your-seasonpackarr-api-token"
```

RSS runs only on its configured interval and always evaluates imports for all
clients. The CLI and API run targeted searches across the configured searchable
indexers. Preview flags apply only to those manual searches.

All three modes scan all configured clients by default. Add `--client default` to
select one client. Use `--url http://127.0.0.1:42069` to set the seasonpackarr base
URL. The `--api` token belongs to seasonpackarr, not Prowlarr.

The CLI prints JSON with scan counts, logical search group count, search request
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

A manual request stays connected until it finishes. Cancel the command to cancel
its run. A reverse proxy must allow long requests. The service cancels automatic
work during shutdown. A cancelled import can have already created hardlinks or
added a stopped torrent; inspect the client before retrying.

## Selection and Coverage

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
4. For targeted search, read enabled, searchable torrent indexers and apply
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
   hardlinks, adds the torrent, verifies data, and starts it through the existing
   client adapter.

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

## Metadata Reuse

SQLite keeps valid torrent metadata for up to seven days, limited to 64 MiB
and 1024 entries. The least recently used entries are removed when needed.
Restarting the service preserves the cache. Changing the Prowlarr URL or API key
clears it on the next discovery run.
Keys include the indexer ID, result GUID (or download link when no GUID exists),
and release title. Different tracker results do not share cached bytes.

Rejected exact matches are cached too. Later runs recheck current inventory,
local files, matching settings, and coverage against those bytes. New episodes or
a lower threshold can make a previous rejection pass without another torrent
download. Search-only dry runs do not read or populate this cache. Invalid torrent
responses are not cached. Cache expiry or eviction can cause another download.
A tracker that replaces metadata under the same result identity can remain stale
until expiry or a connection change. The cache assumes stable tracker result identities.

## Persistent Discovery State

seasonpackarr creates and upgrades its SQLite database automatically. You do not
need to install SQLite or configure another service. Configuration stays in YAML.
The database contains discovery state only, not import jobs, client inventory, or
saved acceptance decisions. Restarting does not resume an interrupted import.

The database location is:

- With a config file: `seasonpackarr.db` beside the active `config.yaml`.
- With `disableConfigFile` and `--config <dir>`: `<dir>/seasonpackarr.db`.
- With `disableConfigFile` and no `--config`: `$XDG_DATA_HOME/seasonpackarr/seasonpackarr.db`,
  or `~/.local/share/seasonpackarr/seasonpackarr.db` when `XDG_DATA_HOME` is unset.
- In the supplied Docker setup: `/config/seasonpackarr.db`, inside the existing volume.

The service account must be able to write the directory and database. Use local
storage, not an NFS or SMB share. Run only one seasonpackarr instance per database.
On Unix, the database permits access only to its owner. On Windows, restrict the
directory to the service account. Torrent metadata and retained links can contain
tracker credentials, so protect database backups as you protect `config.yaml`.

SQLite uses write-ahead logging (WAL). It creates `seasonpackarr.db-wal` and
`seasonpackarr.db-shm` beside the database while it runs. Committed data can still
be in the WAL file, so do not delete or separate these files from the database.
SQLite manages checkpoints automatically; no checkpoint schedule is required.

Expired records are removed at startup and during discovery. Payload limits do
not cap the database file size: SQLite uses space for indexes and can retain free
pages for reuse. No manual database maintenance is normally required.

### Back Up or Restore

1. Stop seasonpackarr cleanly.
2. Copy `config.yaml` and `seasonpackarr.db` to a protected backup location.
3. Start seasonpackarr again.

To restore, stop the service, restore both files with permissions for the service
account, then start it. Do not copy a live database file on its own. A normal
close of the last connection checkpoints the WAL and removes the WAL and shared
memory files. If those files remain after shutdown, do not delete them or assume
the main database file is a complete backup. Start the service normally to allow
recovery, then stop it cleanly before making this backup. If recovery or shutdown
fails, keep all database files together and resolve the reported error first.

An unreadable, corrupt, or unsupported newer database stops startup with an
explicit error. Check directory permissions and free space first. For a corrupt
database, restore a backup. For a newer schema, use a compatible application
version or restore the backup from before the upgrade. The service does not erase
the database or silently fall back to memory.

A database failure during discovery stops further work in that run and appears
in its failure report and service logs. Imports completed before the failure are
not rolled back. Fix the storage problem before running discovery again.

## API Contract

`POST /api/search` uses the same API-token middleware as the webhook endpoints.
Request body:

```json
{"clientname":"default","dryRun":true}
```

Omit `clientname` to scan all configured clients. `dryRun` and `verify` default to
`false`. This endpoint only runs targeted searches.
Use `{"clientname":"default","dryRun":true,"verify":true}` for exact preview.
`verify: true` without `dryRun: true` returns `400`.
The body must contain one JSON object; unknown fields are rejected.

- `200`: completed run report, including any partial failures
- `400`: invalid request, unknown client, or incomplete search configuration
- `401`: missing or invalid API token when authentication is configured
- `409`: another RSS or targeted discovery run is active
- `500`: the discovery database could not be accessed before the run; check service logs

`outcomes[].status` is `candidate`, `would_import`, `imported`, `rejected`, or
`failed`. The report echoes `dryRun` and `verify`. `torrentDownloads` counts
completed metadata retrievals; failed attempts appear as failed outcomes.
`torrentCacheHits` counts metadata cache reads that avoided a retrieval.
Outcomes include client name, release title, indexer ID, reason, reusable episode
count, and total episode count. Counts are `null` until an exact plan establishes the target count.
A known plan with no reusable files reports zero reusable episodes. `failures` reports discovery, client-inventory, query, pagination,
rate-limit, database, and cancellation errors. A `200` response alone does not prove that
all work succeeded.

## Bounds and Limitations

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
  not expanded through guesses. Backfill does not search for shows absent from
  the client and does not remove episode torrents or files.
- Only one RSS or targeted discovery run executes at a time. Imports to the same client endpoint
  are serialized with webhook imports. Matching stays free of client mutations.

## Verification

Covered by Prowlarr HTTP contract fixtures, authenticated API tests, real hardlink
checks in temporary directories, SQLite restart and rollback tests, and a CLI
preview/import smoke test with a database reopen between preview and import.
Live Prowlarr and tracker behavior requires operator verification
with the configured installation. See the [source audit](../references/prowlarr-backfill-api.md).

## Live Installation Check

Use a test client with a few completed episode torrents and a known compatible
season pack on an enabled tracker. Configure the running service with the real
Prowlarr URL and key. Keep `search.rssInterval: "0s"` for the first checks.

1. Run `seasonpackarr search --dry-run --client test --api "<token>"`.
   Confirm series, season, and release variant. Set `search.indexerIDs` and check
   that only those trackers receive queries. Confirm `candidate` outcomes have
   null coverage, `torrentDownloads` is zero, and Prowlarr has no grab requests.
   Check `failures` for tracker or credential errors.
2. Add `--verify` and check reusable episode counts and smart-mode decisions.
   Repeat the exact preview: it should use cached metadata for eligible results.
   Both previews must leave client torrents and local folders unchanged.
3. Review every `would_import` result. Remove both `--dry-run` and `--verify`
   when those selections are correct. `--client` selects the whole client.
   Confirm the pack uses the expected destination, category, and tags; the
   client recognizes reused data and starts the torrent. Verify source episode
   files and pack files are hardlinks, and that existing data is not downloaded
   again. Smart mode can permit missing files or pieces to download.
4. Run the preview again. The imported variant must be rejected as already in
   the client, including equivalent results from other trackers.
5. To test automatic monitoring, configure `rssInterval: "10m"` on a service with
   only the test client. Confirm a poll appears in logs after the interval, with
   `rss: true`. Check that Prowlarr receives no title or season query for the poll
   and that accepted packs use the expected hardlinks and import policy.
   Set `rssInterval: "0s"` and confirm no further poll starts. Then
   choose the intended production interval.

These checks validate installation-specific tracker responses, client paths,
filesystem mounts, and scheduler operation. Automated fixtures already cover the
matching rules, duplicate selection, API authentication, and failure handling.
