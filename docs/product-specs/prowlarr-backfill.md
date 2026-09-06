# Prowlarr Backfill

## Purpose

Find season packs that autobrr missed. Search starts from episode torrents in
configured clients. It uses Prowlarr for tracker access, then uses seasonpackarr's
existing matching, hardlink, and import flow.

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
  interval: "0s"
  requestInterval: "10s" # Minimum: 10s.
```

`prowlarrURL` is the base URL, including the URL base when used. Do not append
`/api/v1`. `apiKey` is Prowlarr's API key. The service does not need a download
client configured in Prowlarr.

`indexerIDs` applies to manual and scheduled runs. An empty list selects all
Prowlarr indexers that are enabled, searchable, and use the torrent protocol.
A nonempty list restricts searches to those Prowlarr IDs. IDs must be unique
positive integers. Missing, disabled, or unsupported selections produce failures;
the run never falls back to unselected trackers. Eligible selected indexers still
run. Read IDs from Prowlarr's `GET /api/v1/indexer` response.

`interval: "0s"` disables automatic runs. Set a positive Go duration, for example
`"24h"`, to opt in. Positive intervals must be at least `"1h"`. The first automatic
run starts after the interval. Each later interval starts when the previous run
finishes. There is no startup run or catch-up burst after downtime.

`requestInterval` sets the minimum time between requests to Prowlarr, including
searches and torrent retrieval within a run. Default and minimum: `"10s"`.
A failed search skips that indexer for the rest of the run. Temporary failures
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

Cooldowns remain in memory across runs. They reset after a service restart or a
Prowlarr URL or API key change. No cooldown state is written to disk.

These limits are project choices, not tracker-specific guarantees.
[cross-seed](https://www.cross-seed.org/docs/basics/options) uses a 30-second bulk
search delay and a minimum one-day search cadence.
[qui](https://getqui.com/docs/features/cross-seed/overview/) uses at least 60 seconds
between Torznab library searches and a minimum 12-hour per-torrent cooldown.
Those controls apply to different units of work from our per-request spacing
and full-run schedule. Keep `"24h"` as a starting schedule for routine backfill.

All search settings reload without a restart. The scheduler detects an interval
change within one second while idle and resets its next run time. An active run
keeps the complete config snapshot with which it started. Its client connections,
matching rules, and import policies stay consistent until that run finishes.

Environment overrides:

- `SEASONPACKARR__SEARCH_PROWLARR_URL`
- `SEASONPACKARR__SEARCH_API_KEY`
- `SEASONPACKARR__SEARCH_INDEXER_IDS` (comma-separated IDs, for example `2,5`)
- `SEASONPACKARR__SEARCH_INTERVAL`
- `SEASONPACKARR__SEARCH_REQUEST_INTERVAL`

## Manual Runs

Start the service, then preview the work:

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

All three modes scan all configured clients by default. Add `--client default` to
select one client. Use `--url http://127.0.0.1:42069` to set the seasonpackarr base
URL. The `--api` token belongs to seasonpackarr, not Prowlarr.

The CLI prints JSON with scan counts, logical search group count, search request
count, completed torrent downloads, metadata cache hits, per-result outcomes,
and operation failures. `coveredEpisodeTorrents` counts episode torrent entries
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
   Group the remaining episodes by normalized series title, year, and season.
   `skipYearCompare` also removes the year from search groups. Reuse a query across
   clients while keeping their source files and import policies separate. A
   covered client does not suppress a search needed by an independent client.
   If no groups remain, do not request Prowlarr indexer discovery or searches.
4. Read enabled, searchable torrent indexers from Prowlarr and apply
   `search.indexerIDs`. Try lower numeric indexer priorities first, with indexer
   ID as the tie-breaker.
5. Use a TV title-and-season query when the indexer advertises those parameters.
   Otherwise use a title and `Sxx` text query. Omit the parsed release year from
   both query forms so trackers can return packs whose names omit it. Keep the
   year in local grouping and compatibility checks, subject to `skipYearCompare`.
   Diagnostic group labels can still include the year. Request TV category `5000` when
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

The process keeps valid torrent metadata for up to seven days, limited to 64 MiB
and 1024 entries. The least recently used entries are removed when needed.
Restarting the service or changing the Prowlarr URL or API key clears the cache.
Keys include the indexer ID, result GUID (or download link when no GUID exists),
and release title. Different tracker results do not share cached bytes.

Rejected exact matches are cached too. Later runs recheck current inventory,
local files, matching settings, and coverage against those bytes. New episodes or
a lower threshold can make a previous rejection pass without another torrent
download. Search-only dry runs do not read or populate this cache. Invalid torrent
responses are not cached. Cache expiry or eviction can cause another download.
A tracker that replaces metadata under the same result identity can remain stale
until expiry or restart. The cache assumes stable tracker result identities.

## API Contract

`POST /api/search` uses the same API-token middleware as the webhook endpoints.
Request body:

```json
{"clientname":"default","dryRun":true}
```

Omit `clientname` to scan all configured clients. `dryRun` and `verify` default to `false`.
Use `{"clientname":"default","dryRun":true,"verify":true}` for exact preview.
`verify: true` without `dryRun: true` returns `400`.
The body must contain one JSON object; unknown fields are rejected.

- `200`: completed run report, including any partial failures
- `400`: invalid request, unknown client, or incomplete search configuration
- `401`: missing or invalid API token when authentication is configured
- `409`: another manual or scheduled backfill run is active

`outcomes[].status` is `candidate`, `would_import`, `imported`, `rejected`, or
`failed`. The report echoes `dryRun` and `verify`. `torrentDownloads` counts
completed metadata retrievals; failed attempts appear as failed outcomes.
`torrentCacheHits` counts metadata cache reads that avoided a retrieval.
Outcomes include client name, release title, indexer ID, reason, reusable episode
count, and total episode count. Counts are `null` until an exact plan establishes the target count.
A known plan with no reusable files reports zero reusable episodes. `failures` reports discovery, client-inventory, query, pagination,
rate-limit, and cancellation errors. A `200` response alone does not prove that
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
- Only one backfill run executes at a time. Imports to the same client endpoint
  are serialized with webhook imports. Matching stays free of client mutations.

## Verification

Covered by Prowlarr HTTP contract fixtures, authenticated API tests, real hardlink
checks in temporary directories, and a CLI preview/import smoke test against the
service handler. Live Prowlarr and tracker behavior requires operator verification
with the configured installation. See the [source audit](../references/prowlarr-backfill-api.md).

## Live Installation Check

Use a test client with a few completed episode torrents and a known compatible
season pack on an enabled tracker. Configure the running service with the real
Prowlarr URL and key. Keep `search.interval: "0s"` for the first checks.

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
5. If scheduling will be used, test a short interval on a service configured
   only with the test client. Confirm the automatic run appears in logs after
   the interval. Set `interval: "0s"` and confirm no further automatic run starts.
   Then select the intended production interval.

These checks validate installation-specific tracker responses, client paths,
filesystem mounts, and scheduler operation. Automated fixtures already cover the
matching rules, duplicate selection, API authentication, and failure handling.
