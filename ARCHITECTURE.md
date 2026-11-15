# Architecture

## System Summary

`seasonpackarr` is a config-driven Go service with a small CLI surface. It finds season packs through authenticated autobrr webhook requests and through opt-in Prowlarr discovery (RSS monitoring and search). It filters releases against configured torrent clients, builds exact import plans from torrent contents, hardlinks reusable episode files into the expected pack folder, and imports the pack into the torrent client.

## Main Runtime Flow

1. `main.go` calls Cobra commands in `cmd/`. Each invocation creates a fresh
   command tree. API commands share connection resolution, transport, and result
   handling. Pack operations use direct `candidate`, `match`, and `import`
   commands. CLI configuration reads are side-effect free and
   share file discovery and environment overrides with the service.
   `setup filter` writes the autobrr filter through `internal/autobrr/`.
2. `cmd/start.go` loads config, logger, notifications, and the SQLite store, creates the inventory module, then starts the HTTP server. Signal cancellation starts a bounded graceful shutdown. The command closes the store after server shutdown.
3. `internal/http/server.go` builds `/api/healthz`, `/api/candidate`, `/api/match`, `/api/import`, and `/api/search`.
4. `internal/http/webhook.go` holds the webhook handlers. A handler decodes the payload, gets the client handle of the request from `internal/inventory/`, calls one operation of the season pack processor, writes the response, and sends the notification.
5. `internal/seasonpack/` is the season pack processor. It has three operations: the candidate check, the match, and the import. `candidate.go` holds the candidate check, which reads the inventory through the client handle. `match.go` parses the torrent bytes and, for an accepted match, builds the import plan with no side effects. `plan_cache.go` keeps the plan of an accepted match for the import. `import.go` uses that plan or builds it again, gets the destination (import root plus layout) from the client adapter, gives the planned links to `internal/placement/`, and imports the pack. `import_lock.go` holds the endpoint import locks. See [docs/design/season-pack-lifecycle.md](docs/design/season-pack-lifecycle.md) and [docs/design/qbittorrent-import-flow.md](docs/design/qbittorrent-import-flow.md).
6. `internal/release/` decides whether a client episode and announced season pack are compatible.
7. `internal/placement/` builds each hardlink target, makes the hardlinks, and answers whether the pack data is complete.
8. `internal/notification/` sends notifications for notable events to each notification destination.

## Domains

### Configuration

- Implemented in `internal/config/` and `internal/domain/config.go`
- Concerns: defaults, config file discovery, strict key validation, validated immutable snapshots, dynamic reload, the generated config file
- Each setting declares its YAML name, its environment variable, and its default in tags on its field in
  `internal/domain/config.go`. At package start, `internal/config/env.go` reads the tags into a key table. The defaults,
  the environment overrides, and the unknown-variable warning use that table. A bad tag panics at package start.
- Runtime readers acquire one snapshot per request or operation. Reload publishes a candidate only after file parsing,
  environment overrides, and validation succeed.
- Durable contract surfaces: `config.yaml`, `schemas/config-schema.json`, [docs/CONFIGURATION.md](docs/CONFIGURATION.md)

### HTTP/API

- Implemented in `internal/http/`
- Concerns: server lifecycle, middleware, auth, health, webhook handlers, Prowlarr discovery runs
- External contract:
  - `POST /api/candidate`
  - `POST /api/match`
  - `POST /api/import`
  - `POST /api/search`
  - `GET /api/healthz/liveness`
  - `GET /api/healthz/readiness`

### autobrr Filter Setup

- Implemented in `internal/autobrr/` and `cmd/setup.go`
- `internal/autobrr/filter.go` holds the filter definition: the base settings
  of a new filter, the candidate check and match External entries, and the
  import action. [docs/AUTOBRR.md](docs/AUTOBRR.md) describes the same filter
  for a manual setup, and a test in `cmd/setup_test.go` compares the two.
- `internal/autobrr/client.go` calls the autobrr REST API. A create stores only
  the base settings, so the client reads the stored filter, sets the External
  entries and actions, and sends it back. An update with `--filter-id` does the
  same for an existing filter. It deletes the old actions first, because the
  autobrr update keeps actions that it does not send. Without `--filter-id`, an
  existing filter with the same name stops the command before any write. Before
  any write, the command reads each other filter and warns about each one that
  calls a seasonpackarr endpoint, because the autobrr filter list has no
  External entries or actions. It never writes to those filters. After each
  write, `internal/autobrr/verify.go` asks autobrr to run the candidate check
  entry through the autobrr External test endpoint. Thus the check uses the
  network position of autobrr. The result is a pass, a failure with its cause,
  or a skip on an autobrr without the endpoint. See
  [the API notes](docs/research/autobrr-filter-api.md) and ADR-0004.

### Release Matching

- Implemented in `internal/release/`, `internal/format/`, `internal/slices/`
- Concerns: comparing resolution, source, release group, cut, edition, repack state, HDR, streaming service, episode identity
- Risk: false positives cause wrong hardlinks; false negatives cause unnecessary downloads

### Torrent Handling

- Implemented in `internal/torrents/`
- Concerns: decoding supplied torrent bytes, deriving torrent identity, and discovering pack files
- `torrents.ParseInfo` returns the validated torrent info, `torrents.Info`. It rejects a torrent whose name is not one
  folder or file name, or whose file path is absolute or leaves the pack folder. The type has no exported fields, so
  only the parse can make a value with content, and no other package reads a name or a file path from the raw info of
  the torrent library. It holds the torrent name, the files (path, size, and the BEP 47 padding mark), the total
  length, and the episode list. It also covers a single-file torrent, for which the episode list gives an error.
- The import plan holds the validated torrent info. The import gives it to the placement module, so the completeness
  check does not parse the torrent again.
- `torrents.InfoHashes` derives the torrent identity. It is a separate function, and the validated type holds no hashes.

### Pack Evaluation and Cache Reuse

- Implemented in `internal/seasonpack/`. One `seasonpack.Processor` owns the candidate check, the match, and the import. The server makes one processor from the inventory store and the logger. The webhook handlers and Prowlarr discovery are its two callers.
- Each operation takes a client handle and a release name. The match also takes a torrent, and the import takes torrent bytes. The handle gives the config snapshot that it was made from, so a stage cannot use the client of one snapshot with the settings of another.
- Each operation returns one outcome: a status code, an error (nil for an accepted season pack), and the coverage (reusable and total episodes) when the torrent episodes were matched. The outcome gives its class: accepted, rejected (a status below `400`, or `445`), or failed. The callers use the class for the log level and for the status of the search report.
- The coverage rule, the plan build, the plan cache, the endpoint import lock, the drops after an import attempt, and the stage logs are private to the package. The processor makes one logger for each call, with the release name and the client name of that call. It sends no notification.
- The match takes the torrent in one of two forms. With the bytes in hand (a webhook request), it reads the torrent first, logs a failed file-detail read, and continues with the client episode files that it could read. With a fetch on demand (discovery), it reads the client episode files first and calls the fetch only when each file-detail read worked and at least one file is on disk. Else it returns a named error with status `468` (file-detail read failed) or `445` (no source on disk). The policies differ on purpose: an announce comes once, and a search result can be tried again in a later run.
- `/api/candidate` reads torrent summaries only. It does not need torrent bytes or file-detail calls.
- `/api/match` parses the announced torrent, maps reusable source files to distinct valid torrent targets, and applies smart mode to exact torrent coverage.
- Each plan build drops a client episode file that is not on disk as a regular file with the size that the client declares, and logs the reason. The webhook match, the import, and search use this one rule. See [docs/design/matching-and-hardlinking.md](docs/design/matching-and-hardlinking.md).
- Exact plan matching parses each episode filename once and uses an indexed compatibility key instead of scanning every source-target pair.
- Exact planning requests candidate file details once. Transmission and Deluge v2 batch hashes in one client call. Deluge v1 uses one session-state preflight and one bulk status call. qBittorrent uses a bounded four-worker adapter pool.
- `internal/inventory/` owns the client connection, the inventory, and the parsed torrent names of each configured client. The candidate check, the match, the import, and search get their client handle and their inventory from it. An inventory is valid for 30 seconds, so the ordered candidate and match checks normally share one client scan.
- A qBittorrent inventory scope makes the scan read only the include values: one `torrents/info` request for each include category and include tag, up to four in parallel, merged by hash in a fixed order. `torrents/info` has no exclude filter, so the adapter drops the excludes after the read, and a scope with excludes only reads every torrent. qBittorrent 4.3 has no tag filter, so there a scope with include tags adds one unfiltered read, and the adapter keeps the torrents with an include tag. `BenchmarkQbitGetTorrents` compares the read shapes. The lookup of the added torrent during the import ignores the scope.
- The inventory module keeps the parsed torrent names of each client endpoint and inventory scope separately from the inventory. An import discards the inventory, but the next scan parses only new names. A scan also reuses the names of the other scopes of its endpoint, so a name in two scopes is parsed once.
- At startup, a server lifecycle task runs the startup read of the inventory module. It reads the inventory of each client once, so the first announce does not log in or parse every name. Clients of one endpoint share the parsed names. The task tries a client that is not ready up to four times, 10 seconds apart, then only logs a warning.
- After each config reload, the server reload loop runs the clean-up of the inventory module. It drops the cached connection and inventory of each client that the config no longer has or configures differently, and the parsed names of endpoints and inventory scopes that no client uses. A scan that overlaps this clean-up adds no new parsed-name key, so it cannot restore a removed one. It closes the Deluge connection of a dropped client. A request that still holds that client connects again.
- Only an accepted match produces an import plan. One function decides the coverage rule of smart mode, for the planned coverage of a plan build and for the achieved coverage after the hardlinks. A rejected match keeps no plan. It returns its coverage when the torrent episodes were matched.
- The processor keeps the import plan of an accepted match for 2 minutes, for both callers. No plan value crosses its interface. The import normally performs no second inventory or file-detail reads.
- A kept plan is valid only for the same client name, torrent identity, release name, client configuration, and fuzzy matching settings. The import builds the plan again when it has no valid one.
- The import has one path: take the endpoint import lock, do the candidate check again, take the kept plan or build it again, apply the coverage rule, make the hardlinks, check the achieved coverage, import into the client, and drop the inventories and plans of the endpoint. Imports to the same client type, host, and port run one at a time.
- After each client import attempt, the import discards the inventories of that endpoint, including aliases, and drops the plans of that endpoint. One deferred call does this and then releases the endpoint import lock, so the order does not depend on two deferred calls. A failed attempt can have added the torrent. When a hardlink source is missing, the import drops the plan and discards the inventory of that one client. A search run discards the inventory of each client before its first read.
- An inventory read that a discard overlaps returns its result to its caller, but does not store it.
- Each plan build returns one compact reason for every unmatched torrent target,
  for the log lines of the match.
  Torrent-client adapters return neutral stage timings for successful and failed
  imports. The season pack processor owns the operator-facing structured logs of
  the stages. The webhook handlers and discovery log only their own start,
  result, and run lines.

### Prowlarr Discovery

- `internal/prowlarr/` owns indexer discovery, capability-aware Torznab queries,
  bounded HTTP reads, request spacing, and torrent proxy retrieval.
- `internal/http/search.go` groups episode inventories by series/season,
  excludes variants already covered by a pack in each client, shares remaining
  searches across years and clients, and gives each result to the season pack
  processor. It accepts one pack per release variant.
- Discovery uses the candidate check for a dry run, the match with a fetch for
  `verify`, and the match with a fetch followed by the import for an import.
  The fetch reads the stored torrent cache or downloads the torrent through
  Prowlarr, one time for each result. The download never runs under the
  endpoint import lock. The import takes the plan of the match from the
  processor, as for the two requests of an announce.
- A run keeps one client handle for each client. The duplicate-variant rule of
  a run, the stored torrent cache, the download counts, the pause rules, and the
  report stay in `internal/http/search.go`.
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
- See [Prowlarr discovery](docs/PROWLARR.md) for operator
  behavior and [the API audit](docs/research/prowlarr-backfill-api.md) for source contracts.

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
  [backup guidance](docs/INSTALL.md#application-data).

### Pack Placement

- Implemented in `internal/placement/`
- Concern: build each hardlink target, make the hardlinks, and answer whether the pack data is complete
- Risk class: high, because pathing mistakes change user disk state
- `placement.Destination` is an import root plus a layout: with a pack folder, or flat. The client adapter decides
  both and returns the value from `ImportDestination`. The same value gives the hardlink targets, the completeness
  check, and the save path of the client import.
- `placement.New` binds a validated torrent info to a destination. The join of import root, pack folder, and file
  path is private to the package.
- `Link` takes the planned links (a source path plus the path of a file in the torrent) and returns one outcome for
  each link in input order: linked, source missing, or failed with its error. It refuses a path that is not a file of
  the torrent, and it links only a directory torrent. It opens the import root one time for each call and makes the
  target folders through `os.Root`. When the import root cannot be made or opened, each link gets the same error.
- `Complete` answers whether each file that is not a padding file is a regular file with the expected size at its
  hardlink target. The comparison is size-only. It reads each hardlink target through `os.Root`, as `Link` does, so
  a hardlink target behind a symlink that `os.Root` rejects is not complete. It makes nothing on disk.
- The import root must be an absolute path. With a relative root, each link fails with an error that names the
  root, `Complete` answers false, and the import returns status `440`.
- The package has no logger and returns no status code. The season pack processor owns the operator logs, the count of
  linked episodes, the one refresh after a missing source, and the status mapping.

### Notifications

- Implemented in `internal/notification/` and `internal/shoutrrrurl/`
- Notification destinations: the native Discord webhook (`notifications.discord`, embeds) and each Shoutrrr URL
  (`notifications.shoutrrr`, plain text through `github.com/nicholas-fedor/shoutrrr`). See ADR-0005.
- The HTTP server gets one `domain.Sender`. For each notification, it reads the current config snapshot, applies the
  global `notificationLevel`, and sends to all destinations at the same time. Each send has its own 30-second timeout,
  and a failure at one destination does not stop the others. `Send` joins one error for each failed destination, and
  the HTTP package logs each on its own line.
- The season pack processor sends no notification. The webhook handlers send `Match` and `Import`, and discovery sends
  `Search` or `RSS` after each import attempt.
- `internal/shoutrrrurl/` names a Shoutrrr destination by list position and scheme (`shoutrrr[1] (gotify)`),
  validates the URLs at config load, and removes the userinfo, path, and query of each configured URL from Shoutrrr
  error text. It is a separate package because `internal/config` validates the URLs and `internal/notification`
  imports `internal/config`.
- `seasonpackarr notification test` (`cmd/notification.go`) loads the config with `config.Load`, which writes no file,
  and sends one test notification to each destination without the notification level.
- Notification sends run in a server-owned task group. Graceful shutdown waits for them until the shutdown deadline, then cancels them.

### Cancellation And Shutdown

- Each webhook passes its request context through planning: the inventory read, the file-detail reads, the wait for the endpoint import lock, and the read of the import destination. The hardlink step takes no context and runs to its end.
- The torrent-client import runs with that context detached from cancellation, so a caller disconnect cannot leave a half-imported torrent stopped. Each adapter bounds its own client calls with timeouts.
- Client imports do not wait for the client's data check. The client checks the data and starts the torrent by itself, so a request finishes within seconds.
- qBittorrent polling stops at its own timeouts. The upstream qBittorrent API is context-free, so an in-flight library call can finish only when its own HTTP behavior returns.
- Process signals start a 15-second graceful shutdown. The server first drains HTTP handlers, then waits for notification tasks within the same deadline.

### Error Diagnostics

- Standard error wrapping keeps `errors.Is` and `errors.As` behavior across modules.
- Unexpected filesystem, torrent-client, notification, and server-operation errors capture one safe stack trace near the failing operation.
- Expected matching and validation outcomes, plus normal context cancellation, do not capture stacks.
- Stack traces are operator log data. HTTP responses and notifications contain the error message only.

## Package Layering

Preferred dependency direction:

1. `cmd/` may depend on `internal/*`
2. `internal/http/` may orchestrate across config, notifications, the season pack processor, inventories, and
   Prowlarr discovery. Its only way into the stages is the three operations of `seasonpack.Processor`. It reads only
   their outcome: no plan, no candidate list, and no source list
3. `internal/seasonpack/` runs the stages across release logic, torrents, inventories, and pack placement. It must not
   import `internal/http/`, and it knows no webhook payload, no notification, and no search report
4. `internal/inventory/` owns the client connections, inventories, and parsed names. Other packages reach that state only through `inventory.Store` and its client handle
5. leaf packages should stay narrow:
   - `internal/release/` should not know HTTP details
   - `internal/placement/` should not know webhook payloads
   - the direction is `internal/torrentclient/` to `internal/placement/` to `internal/torrents/`.
     `internal/placement/` must not import `internal/torrentclient/`
6. `internal/domain/` holds cross-package data shapes and status concepts
7. test helper packages (`internal/logger/loggertest`, `internal/daemontest`,
   `internal/torrentclient/torrentclienttest`) hold test doubles and test helpers
   that must not reach the binary, and some import `testing`, so only `_test.go`
   files may import them

If a change starts pushing transport concerns into matching logic or pack placement, stop and re-check the boundary.

## External Dependencies

- optional autobrr webhook integration, and the autobrr REST API for `setup filter`
- optional Prowlarr API and Torznab tracker access
- qBittorrent, Transmission, and Deluge 1.3/2 native RPC access
- filesystem hardlink support
- Docker/systemd packaging and release automation

Automatic discovery requires Prowlarr RSS, autobrr, or an external scheduler for
targeted Prowlarr searches. Prowlarr RSS and autobrr can run independently or together.

## Testing

Unit and component tests live beside the code and run with `go test ./...`.
Integration tests run each client adapter against real daemons, and end-to-end
tests run real autobrr or Prowlarr, the release image, a real client, and a
seeder. The test layers, conventions, and harness are in
[docs/TESTING.md](docs/TESTING.md).

## Documentation Map

- Domain terms: [GLOSSARY.md](GLOSSARY.md); decisions: [docs/adr/](docs/adr/)
- User guides: [README.md](README.md) links each guide in `docs/`
- Product intent and priorities: [docs/PRODUCT.md](docs/PRODUCT.md)
- Development setup and checks: [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md)
- Design notes for risky subsystems: [docs/design/](docs/design/)
- Primary-source research behind decisions: [docs/research/](docs/research/)
- Database schema: [docs/generated/db-schema.md](docs/generated/db-schema.md)
