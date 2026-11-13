# ARCHITECTURE.md

## System Summary

`seasonpackarr` is a config-driven Go service with a small CLI surface. It accepts authenticated autobrr webhook requests, filters announces against configured torrent clients, builds exact import plans from torrent contents, hardlinks reusable episode files into the expected season-pack folder, and imports the pack into the torrent client.

## Main Runtime Flow

1. `main.go` calls Cobra commands in `cmd/`.
2. `cmd/start.go` loads config, logger, and notifications, then starts the HTTP server.
3. `internal/http/server.go` builds `/api/healthz`, `/api/candidate`, `/api/pack`, and `/api/parse`.
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
  - `POST /api/pack`
  - `POST /api/parse`
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
- `/api/pack` parses the announced torrent, maps reusable source files to distinct valid torrent targets, and applies smart mode to exact torrent coverage.
- Exact planning requests candidate file details once. Transmission and Deluge v2 batch hashes in one client call. Deluge v1 uses one session-state preflight and one bulk status call. qBittorrent uses a bounded four-worker adapter pool.
- Client inventory is cached for 30 seconds, so the ordered candidate and pack checks normally share one client scan.
- Accepted import plans are cached for 2 minutes. `/api/parse` normally performs no second inventory or file-detail reads.
- Cache entries validate client configuration, matching settings, release name, and torrent identity. `/api/parse` rebuilds safely on a cache miss.
- A successful import invalidates the plan and the client inventory.

### File Operations

- Implemented in `internal/files/`
- Concern: create hardlinks safely into target pack directories
- Risk class: high, because pathing mistakes change user disk state

### Notifications

- Implemented in `internal/notification/`
- Current primary output: Discord webhook notifications

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

- autobrr webhook integration
- qBittorrent, Transmission, and Deluge 1.3/2 native RPC access
- filesystem hardlink support
- Docker/systemd packaging and release automation

## Testing Surface

Current explicit test coverage exists in:

- `internal/torrentclient/*_test.go` (unit tests plus environment-gated Deluge V1/V2 live coverage)
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
