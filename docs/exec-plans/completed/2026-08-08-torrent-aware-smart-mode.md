# Torrent-aware smart mode

## goal

Replace external episode-count metadata with the announced torrent as the
authoritative smart-mode denominator, without downloading torrents for obvious
non-matches or repeating a full torrent-client inventory scan across the
candidate, evaluation, and import stages.

## scope

- add an authenticated, announce-only candidate endpoint that rejects releases
  before autobrr downloads the torrent
- require torrent bytes on `/api/pack`; build an exact, side-effect-free import
  plan and apply `smartModeThreshold` to distinct reusable pack episode files
- reuse one short-lived client inventory across candidate and pack evaluation
- reuse an accepted import plan from `/api/pack` in `/api/parse`, with safe
  recomputation after cache misses or process restarts
- remove TVDB/TVMaze provider wiring and metadata configuration
- update webhook setup, config/schema surfaces, architecture, reliability, and
  compact references

## non-goals

- changing release compatibility rules such as resolution, source, group, HDR,
  cut, edition, or repack matching
- persisting caches across process restarts
- making webhook processing asynchronous
- refactoring the status/error classification debt tracked by issue #194

## risks

- **Breaking webhook setup.** Autobrr needs two ordered external webhooks:
  announce-only `/api/candidate`, then torrent-aware `/api/pack`. Existing
  `/api/pack` payloads without torrent bytes fail closed.
- **Stale inventory.** A bounded inventory snapshot can lag client mutations.
  The cache is scoped to the complete client and fuzzy-matching configuration,
  expires quickly, and is invalidated after a successful import.
- **Stale plan.** Source files can disappear between `/api/pack` and
  `/api/parse`. Plan keys include the torrent identity and relevant config;
  hardlink creation remains the final local safety check. A missing plan is
  recomputed.
- **Coverage definition.** Coverage counts distinct valid episode files in the
  torrent, not external season episode counts or bytes. Duplicate client
  releases can fill one target only once, so coverage cannot exceed 100%.

## steps

1. Add endpoint-level regression tests for announce-only candidate matching and
   one inventory read across the candidate and pack stages. [done]
2. Split candidate discovery from file-detail lookup and give the inventory
   cache an explicit bounded lifetime. [done]
3. Add torrent-aware import-plan tests for below-threshold, exact coverage,
   cross-seed deduplication, and side-effect-free `/api/pack`. [done]
4. Cache accepted plans by client, torrent identity, and relevant config; prove
   `/api/parse` performs no second inventory or file-detail reads on a hit and
   recomputes on a miss. [done]
5. Remove metadata providers and configuration, including dependencies and
   generated/default config surfaces. [done]
6. Update webhook, architecture, lifecycle, reliability, and migration docs.
   [done]
7. Run focused Go checks, formatting, repository tests, vulnerability scan, and
   plan review. Move this plan to completed. [done]

## decision log

- 2026-08-08: use two ordered autobrr external checks. Autobrr evaluates them
  sequentially and downloads torrent data only when the second payload refers
  to `TorrentDataRawBytes`, so a failed candidate check does not download the
  torrent.
- 2026-08-08: keep `/api/pack` side-effect free and keep `/api/parse` as the
  only filesystem/client mutation path. Repeating local torrent parsing is
  acceptable; repeating remote client inventory and file-detail reads is not.
- 2026-08-08: key accepted plans by the torrent info hash instead of returning a
  handoff token. Autobrr external filters consume the response status and do not
  pass response bodies into later actions.
- 2026-08-08: caches are performance aids, not correctness requirements. A miss
  recomputes through the same evaluation interface.
- 2026-08-09: keep the processor interface unchanged, but group HTTP transport,
  candidate discovery, exact planning, and import execution in separate files.

## verification notes

- `gofumpt -w .` and `go mod tidy -v` completed. The metadata-only TVMaze and
  Logrus dependencies were removed.
- `go test -v ./...` passed.
- `go test -race ./...` passed.
- `go vet ./...` passed.
- `govulncheck ./...` found no reachable symbol or imported-package
  vulnerabilities. It reports one advisory in an unimported package of a
  required module.
- `jq empty schemas/config-schema.json` and `git diff --check` passed.
- CLI help smoke checks passed for `test candidate` and `test pack`.
- Live webhook and torrent-client smoke tests were not run because this task
  has no configured external client credentials.
- Authenticated HTTP component tests cover candidate, pack, and parse in order,
  cache hits and invalidation, failure status contracts, hardlink conflicts,
  import failure, and retry. They use real torrent parsing and filesystem
  operations with an in-memory torrent client.
