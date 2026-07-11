# ARCHITECTURE.md

## System Summary

`seasonpackarr` is a config-driven Go service with a small CLI surface. Its main job: accept autobrr-triggered webhook requests, validate/authenticate them, compare season-pack announcements against already-downloaded episode releases in configured qBittorrent clients, optionally fetch extra metadata or parse torrent contents, then hardlink matched files into the expected season-pack folder.

## Main Runtime Flow

1. `main.go` calls Cobra commands in `cmd/`.
2. `cmd/start.go` loads config, logger, notifications, metadata providers, then starts the HTTP server.
3. `internal/http/server.go` builds `/api/healthz`, `/api/pack`, and `/api/parse`.
4. `internal/http/processor.go` orchestrates payload decode, auth-adjacent request handling, client inspection, metadata lookups, release matching, and hardlink creation.
5. `internal/release/` decides whether a client episode and announced season pack are compatible.
6. `internal/files/` performs the hardlink operation.
7. `internal/notification/` emits Discord notifications for notable events.

## Domains

### Configuration

- Implemented in `internal/config/` and `internal/domain/config.go`
- Concerns: defaults, config file discovery, dynamic reload, validation, config rendering
- Durable contract surfaces: `config.yaml`, `schemas/config-schema.json`, README config docs

### HTTP/API

- Implemented in `internal/http/`
- Concerns: server lifecycle, middleware, auth, health, webhook handlers
- External contract:
  - `POST /api/pack`
  - `POST /api/parse`
  - `GET /api/healthz/live`
  - `GET /api/healthz/ready`

### Release Matching

- Implemented in `internal/release/`, `internal/format/`, `internal/slices/`
- Concerns: comparing resolution, source, release group, cut, edition, repack state, HDR, streaming service, episode identity
- Risk: false positives cause wrong hardlinks; false negatives cause unnecessary downloads

### Torrent Handling

- Implemented in `internal/torrents/`
- Concerns: obtaining torrent bytes from release names, decoding torrent contents, pack file discovery

### Metadata Enrichment

- Implemented in `internal/metadata/`
- Providers: TVDB and TVMaze
- Purpose: estimate total episodes for smart-mode decisions and cross-check provider disagreement
- Lookup strategy: search with the parsed release title first, then retry with progressively shortened title variants (guarded by name/alias/translation matching), because provider searches often miss long or localized titles
- Caching: successful episode counts are cached for 6 hours keyed by normalized title, year and season; concurrent lookups for the same key are collapsed via singleflight, so cross-seed announce bursts trigger only one provider fetch

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
2. `internal/http/` may orchestrate across config, metadata, notifications, release logic, torrents, files
3. leaf packages should stay narrow:
   - `internal/release/` should not know HTTP details
   - `internal/files/` should not know webhook payloads
   - `internal/metadata/` should not know HTTP transport internals
4. `internal/domain/` holds cross-package data shapes and status concepts

If a change starts pushing transport concerns into matching logic or file ops, stop and re-check the boundary.

## External Dependencies

- autobrr webhook integration
- qBittorrent API access
- TVDB API
- TVMaze API
- filesystem hardlink support
- Docker/systemd packaging and release automation

## Testing Surface

Current explicit test coverage exists in:

- `internal/release/release_test.go`
- `internal/format/format_test.go`
- `internal/metadata/metadata_test.go`
- `internal/metadata/titles_test.go`
- `internal/metadata/tvdb_test.go` (requires `TVDB_API_KEY` env var, skipped otherwise)
- `internal/metadata/tvmaze_test.go`
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
