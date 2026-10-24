# ARCHITECTURE.md

## System Summary

`seasonpackarr` is a config-driven Go service with a small CLI surface. It discovers packs through opt-in Prowlarr RSS polling, manual searches, and authenticated autobrr webhook requests. It filters releases against configured torrent clients, builds exact import plans from torrent contents, hardlinks reusable episode files into the expected season-pack folder, and imports the pack into the torrent client.

## Main Runtime Flow

1. `main.go` calls Cobra commands in `cmd/`. Each invocation creates a fresh
   command tree. API commands share connection resolution, transport, and result
   handling. Pack operations use direct `candidate`, `match`, and `import`
   commands. CLI configuration reads are side-effect free and
   share file discovery and environment overrides with the service.
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

- Each webhook passes its request context through planning and hardlinking.
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

### Files

- `<file>_test.go` tests the code in `<file>.go`. A large set can split by
  aspect: `<file>_<aspect>_test.go` (`search_cooldown_test.go`,
  `client_rss_test.go`, `processor_candidate_benchmark_test.go`).
- Tests that drive several files through one entry point live with the entry
  point. HTTP endpoint tests live in `internal/http/processor_handlers_test.go`.
- `fixtures_test.go` holds the fakes and fixtures that more than one test file
  of the package uses. A fake that one file uses stays in that file.
- `<file>_integration_test.go` holds tests against real external services.
  Their shared fixtures live in `fixtures_integration_test.go`.
- `internal/http/cli_test.go` holds the end-to-end tests. They build the binary
  with `buildCLI` and run it against the HTTP fixtures. They need no external
  service and run in the default suite.

### Names

- Tests are `Test<Subject>_<Behavior>`, or `Test<Subject>` when one test covers
  the whole contract. Behavior is a verb phrase (`ResumesPartialPack`), or a
  noun phrase for the area the test covers (`InvalidAddresses`).
- The subject is one of:
  - the function under test (`TestEpisodeFileFromFiles_`), or a short type
    name plus the method (`TestQbitImport_` for `qbitClient.Import`). When a
    package has one main type, the method alone is enough
    (`TestImportSeasonPack_` for `processor.importSeasonPack`).
  - a type, when the test covers behavior across several of its methods
    (`TestClient_`, `TestStore_`, `TestAPIClient_`)
  - `<Name>Command` for a CLI command (`TestOperationCommand_`)
  - `<Route>Endpoint` for one HTTP route (`TestImportEndpoint_`), or
    `Endpoints` for a test across routes
  - a named feature (`TestSearch_`, `TestRSS_`, `TestInventory_`, `TestCLI_`)
- Subtests use short, lowercase phrases. Names and acronyms keep their case
  (`deluge requires savePath`, `contains HDR`). A named table is `tests`, and
  the loop variable is `tt`. A table written inline in the `range` needs no name.
  A map table names its key and value instead (`for name, mutate := range tests`).
- Test doubles are `fake<Thing>` (`fakeTorrentClient`, `fakeQbitAPI`), and their
  methods use the receiver `f`. Fixtures and recorded data keep descriptive
  names (`searchFixture`, `capturedRequest`).
- Common helper prefixes:
  - `new<Thing>`: builds a client, fixture, or fake
  - `write<Thing>`: creates files on disk
  - `require<Fact>` / `assert<Fact>`: checks a fact and stops the test (fails or
    skips it) / continues on failure
  - `wait<Condition>`: polls an external service until a condition is true

### Assertions

- Assertions use testify. Use `require` for setup and for checks that make the
  rest of the test meaningless. Use `assert` for independent facts about one
  final state or for table rows, so every failure shows.
- `require` and `t.Fatal` stop the test only from the test goroutine. Inside
  handlers served by `httptest.NewServer`, fixture callbacks that such a
  handler runs (`respond`, `beforeSearch`), and other goroutines, use `assert`.
  This also applies to the helpers they call (`postRaw`, not `postJSON`).
  A handler that the test calls directly through `ServeHTTP` and
  `httptest.NewRecorder` runs on the test goroutine and can use `require`.
- Benchmarks keep `if err != nil { b.Fatal(err) }` in measured loops.
- Log assertions use `internal/logger/loggertest`.
- Every Go file groups imports as standard library, this module, then external
  modules.

### Torrent-client integration tests

Integration tests run against real daemons. They use the `integration` build
tag, so `go test ./...` does not run them. The harness in
`internal/torrentclient/testdata/harness/` starts the daemons and runs them,
locally and in the integration workflow.

- `internal/torrentclient/<client>_integration_test.go` holds one client's
  tests. `fixtures_integration_test.go` holds the shared fixtures, for example
  the environment names, `requireDaemon`, the pack writers, `waitFor`, the
  seeder helpers, and the shared assertions.
- Names are `Test<Client>Daemon_<Behavior>` (`TestQbitDaemon_ResumesPartialPack`).
  `Daemon` separates them from the unit tests of the same adapter.
- Each test calls `requireDaemon` first. It skips the test when the client's
  gate variable or `SEASONPACKARR_TEST_IMPORT_DIR` is not set. When
  `SEASONPACKARR_TEST_REQUIRE_DAEMONS=1` (strict mode), it fails the test
  instead and names the missing settings. Skips for features that a daemon
  version does not support stay skips in strict mode.
- `packName` names each pack after the test, so tests do not share pack
  folders. Deluge tests add the client type, because Deluge 1 and Deluge 2 runs
  can share one import folder.
- Each client's `import<Client>Pack` helper registers the torrent removal with
  `t.Cleanup` before the import. The cleanup asserts with `assertRemoved` that
  the daemon no longer holds the torrent, so a run cannot leave a torrent
  behind for the next one. Pack data stays on disk for inspection.
- Cleanup calls that take a context use `cleanupContext`, because `t.Context`
  is canceled before cleanup functions run.
- `Test<Client>Daemon_DownloadsMissingEpisodes` checks the core promise with a
  real download, for a missing first and a missing last episode. `newSeeder`
  controls the seeder daemon with go-qbittorrent directly, not through an
  adapter. `seeder.seed` writes the full pack into the seed folder and seeds it.
  `writeLinkedPack` writes the source episodes below the import folder and
  hardlinks all except the missing one into the pack folder. After the import,
  `newPeerAddress` resolves the client's host name and listen port to the
  `<ip>:<port>` address, and `waitSeededDownload` connects the seeder to it
  with `addPeers`.
  `assertDownloadedPack` checks the file content, that reused episodes keep
  the inode of their source files, and that only the missing episode
  downloaded.

The import folder must have the same path for the test process and the daemon.

| Client | Gate variable | Other variables |
| --- | --- | --- |
| all | `SEASONPACKARR_TEST_IMPORT_DIR` | `SEASONPACKARR_TEST_REQUIRE_DAEMONS` (`1` turns on strict mode) |
| qBittorrent | `SEASONPACKARR_TEST_QBIT_HOST` | `_QBIT_USER`, `_QBIT_PASS` |
| Transmission | `SEASONPACKARR_TEST_TRANSMISSION_HOST` | `_TRANSMISSION_USER`, `_TRANSMISSION_PASS` |
| Deluge | `SEASONPACKARR_TEST_DELUGE_TYPE` (`deluge-v1` or `deluge-v2`) | `_DELUGE_HOST` (`127.0.0.1`), `_DELUGE_PORT` (`58846`), `_DELUGE_USER` (`seasonpackarr`), `_DELUGE_PASS` (`integration`) |
| Seeder (download tests) | `SEASONPACKARR_TEST_SEEDER_HOST` and `SEASONPACKARR_TEST_SEED_DIR` | `_SEEDER_USER`, `_SEEDER_PASS` |

The seed folder must be on the same volume as the import folder. The seeder
must reach the client under test at the host name in the client's host
setting, so the download tests need the harness network.

### Commands

```sh
go test ./...
go test -race ./...
go test -tags=integration -count=1 -v -run 'Daemon_' ./internal/torrentclient
go test -tags=integration -count=1 -v -run 'Qbit' ./internal/torrentclient
go test -tags=integration -count=1 -v -run 'Transmission' ./internal/torrentclient
go test -tags=integration -count=1 -v -run 'Deluge' ./internal/torrentclient
```

`-run 'Daemon_'` runs only the integration tests. The client-specific commands
run that adapter's unit tests and integration tests together. Every test of an
adapter has the client name in its name (`TestBuildDelugeSettings`,
`TestNewTransmissionClient_UsesBasicAuth`), so the patterns are not anchored. `-count=1`
prevents cached results from hiding changes in an external service.

### Harness

`internal/torrentclient/testdata/harness/run.sh` runs the integration tests
against pinned daemon versions in Docker Compose. It needs only Docker. Local
runs and the integration workflow use the same command (ADR-0001).

```sh
internal/torrentclient/testdata/harness/run.sh                # default entries, the CI set
internal/torrentclient/testdata/harness/run.sh all            # default entries and the middle versions
internal/torrentclient/testdata/harness/run.sh qbit-4.3.9     # one or more named entries
internal/torrentclient/testdata/harness/run.sh list [all]     # entries as a JSON array
```

- An entry is `<client>-<version>`. The default entries are the oldest and
  newest supported version of each client. `all` adds the middle qBittorrent
  versions. Bump the newest pins by hand. An automatic bump tool would also
  move the oldest pins.
- For each entry, the runner pulls or builds the images, starts the daemon,
  waits for its health check, and runs that client's `Test<Client>Daemon_`
  tests with `-tags=integration -count=1` in strict mode.
- The tests run in a Go container that uses the Go image of the release
  `Dockerfile`. It shares one named volume at `/data` with the daemons, and
  the import folder is `/data/import`. The tests and the daemons run as uid
  1000, so a daemon can write into the packs that the tests create.
- Every entry starts with fresh containers and volumes, and the runner removes
  them after the entry, also when it fails. Only the Go cache volume
  `seasonpackarr-harness-go-cache` stays.
- A failed entry writes the daemon logs to `testdata/harness/artifacts/<entry>/`,
  which git ignores.
- The compose project name comes from the checkout path, so two worktrees can
  run the harness at the same time.
- `compose.yaml` holds the Go test service and the seeder. Each client adds a
  `compose.<client>.yaml` overlay with its daemon and its test settings.
- The seeder is hotio qBittorrent 5.2.4 (`SEEDER_IMAGE` in `run.sh`) with the
  committed `qBittorrent.conf`. It runs in every entry, and the tests start
  only after its health check. Its seed folder is `/data/seed`. The `qbit`
  service extends the seeder service, so both qBittorrent daemons start the
  same way. Bump the seeder pin with the newest qBittorrent entry.
- Daemons use committed credentials, and DHT, PEX and LPD are off. qBittorrent
  uses hotio images and `qbittorrent/qBittorrent.conf` (`admin:integration`).
  hotio publishes 4.3.9 only under the moving `legacy` tag, so the runner
  pins it by digest. Transmission uses linuxserver images, which set the RPC
  credentials (`admin:integration`) from the `USER` and `PASS` environment
  variables. The runner pins their build tags (`4.1.3-r0-ls363`), because the
  plain version tags move with each weekly rebuild. The daemon starts from
  `transmission/settings.json`, which also turns off port forwarding and the
  incomplete and watch folders of the image defaults. No maintained image has
  Deluge 1.3.15 or 2.0.3, so the runner builds `deluge/Dockerfile` from Debian
  packages: 1.3.15 from the buster archive, 2.0.3 from bookworm, and 2.2.0
  from trixie. The build args are the Debian release and the exact `deluged`
  package version. The base image follows the Debian release, and the build
  fails when the mirror no longer has the pinned package version. `deluge/entrypoint.sh` writes the
  auth entry (`seasonpackarr:integration:10`) and `core.conf`, then starts
  `deluged` on all interfaces.

`.github/workflows/integration.yml` runs the harness in CI. It runs on pull
requests and pushes to `develop` that change the torrent client package, the
internal packages that its tests import, `go.mod`, `go.sum`, `Dockerfile`, or
the workflow. A manual run has a `full` input.

- A setup job reads the matrix from `run.sh list`, or `run.sh list all` when
  `full` is set. Each entry is one parallel job that runs `run.sh <entry>`, so
  a new runner entry needs no workflow change.
- Jobs do not retry, and one failed entry does not cancel the others. A failed
  job uploads its daemon logs as the `daemon-logs-<entry>` artifact.
- The workflow sets `HARNESS_GHA_CACHE=1`, and the Deluge overlay then uses the
  GitHub Actions layer cache with one scope for each package version. Local
  runs leave it unset, because the default docker build driver cannot export a
  cache.

### Coverage

Packages without tests: `internal/api`, `internal/buildinfo`,
`internal/domain`, `internal/logger`, `internal/logger/loggertest`,
`internal/notification`.

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
