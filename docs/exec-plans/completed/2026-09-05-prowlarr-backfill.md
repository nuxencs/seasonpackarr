# Prowlarr season-pack backfill

Follow-up: [backfill download controls](2026-09-05-backfill-download-controls.md)
adds tracker selection and makes default dry runs search-only. Exact preview now
requires `--dry-run --verify`. The notes below record the initial implementation.


Status: Complete. Implementation and fixture verification finished.

## Goal

Find season packs that autobrr missed. Use episodes already in configured
torrent clients to identify searches, query enabled torrent trackers through
Prowlarr, and import compatible packs with reusable local files.

Core decision: None. This work serves the backfill goal.

## Scope

- Scan configured clients through the existing torrent-client adapters.
- Identify episode torrents and group searches by normalized series title,
  year, and season. Keep each client's source inventory and import policy separate.
- Retain release variants for compatibility checks. A shared search must not
  merge incompatible source files.
- Use Prowlarr for tracker access and torrent retrieval. Verify its API and
  indexer capabilities before selecting request details.
- Pass results through the existing candidate, exact matching, and import flow.
- Provide a preview that reports proposed imports and rejection reasons without
  creating hardlinks or adding torrents.
- Support cancellation, bounded requests, and visible partial failures.
- Update configuration defaults, schema, operator docs, and API/CLI contracts.

## Success criteria

- Multiple episodes of the same series and season produce one logical search
  per selected tracker, subject to required pagination or capability fallback.
- Different series, years, seasons, and release variants cannot supply incorrect
  source files to an import.
- A compatible pack missed by autobrr reaches the configured client through the
  existing hardlink and import path.
- Existing matching and coverage rules apply unless the user selects a stricter
  backfill policy.
- Preview leaves the filesystem and torrent client unchanged.
- Repeat runs skip duplicates. Concurrent backfill runs cannot import the same
  result twice. Backfill and webhook overlap has an explicit duplicate policy.
- A failed tracker does not discard successful results from other trackers.
- Operators can distinguish no results, rejected results, failed searches, and
  successful imports.

## Non-goals

- Searching for series with no episode torrents in a configured client.
- Quality upgrades or a new release-ranking system.
- Sonarr integration or an external series metadata database.
- Combining files across separate client configurations.
- Removing episode torrents or their files.
- A web interface.

## Technical givens

Use the existing Go service, CLI, client adapters, matching rules, and import
policies. Prowlarr is the tracker integration. Protect new API routes with the
existing API-token middleware. Do not add durable job storage without a concrete
requirement. Keep candidate checks free of torrent downloads and file-detail reads.

## Confirmed product choices

1. Both on-demand and scheduled runs. The schedule is opt-in.
2. One pack per release variant. An equivalent pack already in the client blocks
   imports from other trackers. Cross-tracker duplication belongs to other apps.
3. Respect existing smart-mode settings.
4. Use Prowlarr indexer priority and stable result order to try candidates. Accept
   the first exact match per variant; do not introduce quality ranking.

## Risks

- Existing compatible packs block imports across trackers, regardless of info
  hash. This policy matches the confirmed scope and remains in effect.
- Tracker search capabilities, paging, and rate limits differ. Verify Prowlarr's
  behavior and apply bounded work and backoff.
- Torrent names may lack a reliable season or title. Ignore these entries;
  report scanned and eligible episode counts and document the eligibility rule.
  Do not guess external identifiers.
- Torrent summaries do not expose download completion. Do not assume that a
  listed episode has complete data. Preserve existing import verification.
- Prowlarr credentials and tracker download URLs must not appear in logs.
- Long search runs must follow service cancellation and shutdown behavior.

## Steps and buckets

1. Manual preview: verify Prowlarr contracts, configure the connection, group
   client episodes, search selected trackers, and report exact match outcomes.
   Done when an API/CLI preview exercises the complete path without mutations.
2. Backfill import: import accepted results through shared processing, implement
   the agreed selection and duplicate policy, and report per-result outcomes.
   Done when an end-to-end fixture verifies file reuse and repeat-run behavior.
3. Automatic execution: add an optional interval and prevent
   overlapping runs. Done when schedule, cancellation, and shutdown checks pass.
4. Operator handoff: finish configuration and usage docs and run affected checks.
   Done when the documented preview and import commands pass smoke checks.

## Decision log

- 2026-09-05: User requested client-inventory-driven Prowlarr backfill for missed
  autobrr announcements.
- 2026-09-05: Repository inspection supports reuse of existing client inventory,
  release comparison, exact planning, and import execution. No code changes yet.
- 2026-09-05: User confirmed both triggers, an opt-in schedule, one pack per
  release variant across trackers, and existing smart-mode coverage settings.

## Build log

### Bucket 1: Manual preview, aligned

Built the Prowlarr adapter, config validation and environment overrides, shared
season queries, authenticated `/api/search`, and `seasonpackarr search --dry-run`.
Preview applies exact matching and leaves the filesystem and client unchanged.

The API audit uses pinned Prowlarr source. TV category filtering applies only
when advertised, so uncategorized trackers are not excluded.

### Bucket 2: Backfill import, aligned

Accepted packs use the existing hardlink and import path. Existing packs and
selections made during the run block equivalent variants across trackers.
Independent client configurations retain separate source files and import policies;
aliases of the same endpoint share duplicate protection.

Added an import lock shared with webhooks. Client mutation attempts invalidate
endpoint inventories and plans, including failed attempts. An in-flight inventory
scan cannot restore data invalidated by an import. This is a necessary concurrency
change, not a change to release compatibility or coverage.

### Bucket 3: Automatic execution, aligned

Added an opt-in interval, reload handling, one-run-at-a-time admission, and
cancellation through the server's task-group lifecycle. Prowlarr `Retry-After`
deadlines survive between runs in memory. No persistent queue or database added.

### Bucket 4: Operator handoff, aligned

Updated README, example and generated config, schema, API and product docs,
architecture, quality notes, and compact operator references. The real CLI preview
and import commands pass against the authenticated service handler, Prowlarr HTTP
fixtures, a controlled torrent-client boundary, and real temporary hardlinks.

No product-scope drift. Live tracker validation and static-tool limitations are
reported below.

## Verification notes

Passed:

- `go test -v ./...`
- `go test -race ./internal/prowlarr ./internal/http ./internal/config ./cmd`
- Focused HTTP/CLI race checks after the final shutdown-lifecycle change.
- `go build -o /tmp/seasonpackarr-search-20260905 .`
- `go run . search --help`
- `TestSearchCLI_PreviewAndImport`, which invokes the shipped CLI end to end.
- `govulncheck ./...`: no reachable vulnerabilities. Four findings exist only
  in required modules, outside the application's call paths.
- `go fix` for affected packages, `gofumpt -w .`, and `git diff --check`.

Dead-code check resolved on 2026-09-05:

- The installed v0.43.0 binary was built with Go 1.26.2 and could not parse the
  active Go 1.27.1 toolchain. Tagged v0.49.0 had a separate confirmed upstream
  generic-method bug: [golang/go#80973](https://github.com/golang/go/issues/80973).
- A minimal program that boxes `*math/rand/v2.Rand` reproduced the same panic.
  Upstream fix `2e922938d07f0ab6689b9bc341b9121d3ce1357b` removes the panic and
  still reports a deliberately unused function.
- Installed `golang.org/x/tools/cmd/deadcode` at
  `v0.49.1-0.20260828025639-2e922938d07f`, built with Go 1.27.1. This is a pinned
  upstream revision after v0.49.0, not a local patch. No repo dependency changes.
- `deadcode ./...` now completes. Existing production-unreachable functions:
  `EpisodeMatcher.Len`, `MatchEpToSeasonPackEp`, `IsValidEpisodeFile`, and `Dedupe`.
  `deadcode -test ./...` reports only `EpisodeMatcher.Len`. No search-code findings.
  These functions were not removed as part of the tool update.

Integration limit:

- No live Prowlarr instance or private tracker was exercised. External boundaries
  use source-backed HTTP fixtures. The torrent import test uses real filesystem
  hardlinks and a controlled client adapter. Operator preview remains the live
  verification step for a configured installation.
