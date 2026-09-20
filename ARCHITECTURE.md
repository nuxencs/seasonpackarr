# ARCHITECTURE.md

## System Summary

`seasonpackarr` is a config-driven Go service with a small CLI surface. It discovers packs through opt-in Prowlarr RSS polling, manual searches, and authenticated autobrr webhook requests. It filters releases against configured torrent clients, builds exact import plans from torrent contents, hardlinks reusable episode files into the expected season-pack folder, and imports the pack into the torrent client.

## Main Runtime Flow

1. `main.go` calls Cobra commands in `cmd/`.
2. `cmd/start.go` loads config, logger, notifications, and the SQLite store, then starts the HTTP server. Signal cancellation starts a bounded graceful shutdown. The command closes the store after server shutdown.
3. `internal/http/server.go` builds `/api/healthz`, `/api/candidate`, `/api/match`, `/api/import`, and `/api/search`.
4. `internal/http/processor_*.go` keeps each processing stage together. The handler file owns payload decode and responses. The candidate file owns announce-only matching and inventory caching. The plan file parses torrent bytes and builds an exact side-effect-free plan. The import file reuses or rebuilds that plan, resolves the client import destination, hardlinks matched files, and imports the pack. See [docs/design-docs/qbittorrent-import-flow.md](docs/design-docs/qbittorrent-import-flow.md).
5. `internal/release/` decides whether a client episode and announced season pack are compatible.
6. `internal/files/` performs the hardlink operation.
7. `internal/notification/` emits Discord notifications for notable events.

## Domains

### Configuration

- Implemented in `internal/config/` and `internal/domain/config.go`
- Concerns: defaults, config file discovery, validated immutable snapshots, dynamic reload, config rendering
- Runtime readers acquire one snapshot per request or operation. Reload publishes a candidate only after file parsing,
  environment overrides, and validation succeed.
- Durable contract surfaces: `config.yaml`, `schemas/config-schema.json`, README config docs

### HTTP/API

- Implemented in `internal/http/`
- Concerns: server lifecycle, middleware, auth, health, webhook handlers
- External contract:
  - `POST /api/candidate`
  - `POST /api/match`
  - `POST /api/import`
  - `POST /api/search`
  - `GET /api/healthz/liveness`
  - `GET /api/healthz/readiness`

### Release Matching

- Implemented in `internal/release/`, `internal/format/`, `internal/slices/`
- Concerns: comparing resolution, source, release group, cut, edition, repack state, HDR, streaming service, episode identity
- Risk: false positives cause wrong hardlinks; false negatives cause unnecessary downloads

### Torrent Handling

- Implemented in `internal/torrents/`
- Concerns: decoding supplied torrent bytes, deriving torrent identity, and discovering pack files

### Pack Evaluation and Cache Reuse

- `/api/candidate` reads torrent summaries only. It does not need torrent bytes or file-detail calls.
- `/api/match` parses the announced torrent, maps reusable source files to distinct valid torrent targets, and applies smart mode to exact torrent coverage.
- Exact plan matching parses each episode filename once and uses an indexed compatibility key instead of scanning every source-target pair.
- Exact planning requests candidate file details once. Transmission and Deluge v2 batch hashes in one client call. Deluge v1 uses one session-state preflight and one bulk status call. qBittorrent uses a bounded four-worker adapter pool.
- Client inventory is cached for 30 seconds, so the ordered candidate and match checks normally share one client scan.
- Accepted import plans are cached for 2 minutes. `/api/import` normally performs no second inventory or file-detail reads.
- Cache entries validate client configuration, matching settings, release name, and torrent identity. `/api/import` rebuilds safely on a cache miss.
- Imports to the same client type, host, and port are serialized. The candidate gate is checked again before a cached plan is used.
- Every client import attempt invalidates plans and inventories for that endpoint, including aliases. A failed attempt may already have added the torrent.
- An inventory scan invalidated during an import cannot publish its older data back into the cache.
- Exact plans retain one compact reason for every unmatched torrent target.
  Torrent-client adapters return neutral stage timings for successful and failed
  imports. The HTTP processor owns the operator-facing structured logs.

### Prowlarr Discovery

- `internal/prowlarr/` owns indexer discovery, capability-aware Torznab queries,
  bounded HTTP reads, request spacing, and torrent proxy retrieval.
- `internal/http/search.go` groups episode inventories by series/season,
  excludes variants already covered by a pack in each client, shares remaining
  searches across years and clients, and feeds results through existing candidate,
  exact-plan, and import processing. It accepts one pack per release variant.
- `POST /api/search` and `cmd/search.go` expose search-only dry runs, exact previews, and imports. The
  request context owns a manual run; there is no persistent job queue.
- `internal/http/search_schedule.go` polls RSS on an opt-in interval from the
  server lifecycle context. Targeted searches are manual only. All discovery runs
  share an overlap guard.
- `internal/http/rss.go` reads recent feeds once per indexer and pages back to the
  previous poll when possible. `internal/state/rss.go` retains bounded relevant
  listings for later coverage checks. SQLite preserves feed state across restarts.
  Checkpoints and candidates commit together before evaluation. Runs with an
  unavailable client discard their feed working set instead of advancing it.
  Only the scheduler can start an RSS poll. Manual CLI/API requests always use targeted search.
- `internal/state/metadata.go` bounds durable metadata reuse to seven days,
  64 MiB, and 1024 entries. Exact runs check local sources before retrieval
  and rebuild decisions from current inputs.
- Each run keeps one config snapshot and applies the configured indexer allowlist.
  RSS selects RSS-capable indexers; targeted search selects searchable indexers.
  Both modes share one connection whose request spacing survives across runs.
- SQLite retains retry deadlines across runs and restarts. HTTP 429, temporary HTTP
  errors, and transport failures pause the affected indexer. Discovery failures
  pause the whole Prowlarr connection. Connection changes reset discovery state
  atomically. Requests are not retried automatically.
- See [Prowlarr backfill](docs/product-specs/prowlarr-backfill.md) for operator
  behavior and [the API audit](docs/references/prowlarr-backfill-api.md) for source contracts.

### Database

- `internal/state/` owns the embedded SQLite driver, schema migrations, expiry,
  metadata eviction, and atomic RSS snapshots. There is no alternative backend.
- `cmd/start.go` creates `seasonpackarr.db` beside the active config. Environment-only
  setups use the explicit config directory or the user data directory.
- The application owns one SQL connection with WAL journaling, SQLite-managed
  automatic checkpoints, and FULL synchronization. Each commit synchronizes the
  WAL to preserve acknowledged writes through an OS crash or power loss, subject
  to the storage system honoring synchronization. Transactions never cover
  tracker or torrent-client requests.
- `PRAGMA user_version` tracks embedded, transactional migrations. Startup rejects
  corrupt or newer databases. A storage failure stops discovery, not a fallback
  to volatile storage.
- Prowlarr connection identity is an HMAC-SHA-256 fingerprint of the URL, keyed
  by the API key. Cached torrent bytes and result links remain sensitive data.
- Configuration, client inventory, coverage decisions, and exact import plans are
  not persisted. There is no durable job queue or multi-process scheduler.
- See [schema documentation](docs/generated/db-schema.md) and
  [backup guidance](docs/product-specs/prowlarr-backfill.md#persistent-discovery-state).

### File Operations

- Implemented in `internal/files/`
- Concern: create hardlinks safely into target pack directories
- Risk class: high, because pathing mistakes change user disk state

### Notifications

- Implemented in `internal/notification/`
- Current primary output: Discord webhook notifications
- Notification sends run in a server-owned task group. Graceful shutdown waits for them until the shutdown deadline, then cancels them.

### Cancellation And Shutdown

- Each webhook passes its request context through planning and torrent-client operations.
- Transmission and Deluge derive adapter timeouts from that request context.
- qBittorrent polling stops on cancellation. The upstream qBittorrent API is context-free, so an in-flight library call can finish only when its own HTTP behavior returns.
- Process signals start a 15-second graceful shutdown. The server first drains HTTP handlers, then waits for notification tasks within the same deadline.
- A future persistent import worker must derive each execution context from the worker lifecycle, not from the intake request. The request context can cover validation and durable job admission only.

### Error Diagnostics

- Standard error wrapping keeps `errors.Is` and `errors.As` behavior across modules.
- Unexpected filesystem, torrent-client, notification, and server-operation errors capture one safe stack trace near the failing operation.
- Expected matching and validation outcomes, plus normal context cancellation, do not capture stacks.
- Stack traces are operator log data. HTTP responses and notifications contain the error message only.

## Package Layering

Preferred dependency direction:

1. `cmd/` may depend on `internal/*`
2. `internal/http/` may orchestrate across config, notifications, release logic, torrents, and files
3. leaf packages should stay narrow:
   - `internal/release/` should not know HTTP details
   - `internal/files/` should not know webhook payloads
4. `internal/domain/` holds cross-package data shapes and status concepts

If a change starts pushing transport concerns into matching logic or file ops, stop and re-check the boundary.

## External Dependencies

- optional autobrr webhook integration
- optional Prowlarr API and Torznab tracker access
- qBittorrent, Transmission, and Deluge 1.3/2 native RPC access
- filesystem hardlink support
- Docker/systemd packaging and release automation

Automatic discovery requires Prowlarr RSS, autobrr, or an external scheduler for
targeted Prowlarr searches. Prowlarr RSS and autobrr can run independently or together.

## Testing Surface

Test functions use `Test<Subject>` when the subject names the complete contract
or groups closely related cases. They use `Test<Subject>_<Behavior>` for a
distinct invariant. Subtest names use short, lowercase phrases. Test files use
lowercase responsibility names and one of these suffixes:

- `<subject>_test.go` for unit, component, and contract coverage
- `<subject>_integration_test.go` for tests against real external services

Hermetic tests, including HTTP tests backed by `httptest`, run in the default
suite and use responsibility-based filenames. Torrent-client integration tests
use the `integration` build tag, require explicit
`SEASONPACKARR_TEST_*` connection settings, and are not part of the default CI
workflow.

Canonical commands:

```sh
go test ./...
go test -race ./...
go test -tags=integration -count=1 -v ./internal/torrentclient
go test -tags=integration -count=1 -v -run '^TestQbit' ./internal/torrentclient
go test -tags=integration -count=1 -v -run '^TestTransmission' ./internal/torrentclient
go test -tags=integration -count=1 -v -run '^TestDeluge' ./internal/torrentclient
```

The client-specific commands run that adapter's unit tests and tagged
integration tests together. `-count=1` prevents cached results from hiding
changes in an external service.

Current explicit test coverage exists in:

- `internal/torrentclient/*_test.go` (unit tests plus tagged integration coverage for supported clients)
- `internal/release/release_test.go`
- `internal/format/format_test.go`
- `internal/http/processor*_test.go`
- `internal/payload/payload_test.go`
- `internal/slices/slices_test.go`

High-value regression targets:

- fuzzy matching options
- smart mode threshold behavior
- torrent parsing path/name mismatches
- episode-to-pack file matching
- config migrations/default changes

## Documentation Map

- Design beliefs and lifecycle docs: `docs/design-docs/index.md`
- Product/user specs: `docs/product-specs/index.md`
- Plans and tech debt: `docs/PLANS.md`
- Risk posture: `docs/QUALITY_SCORE.md`, `docs/RELIABILITY.md`, `docs/SECURITY.md`
