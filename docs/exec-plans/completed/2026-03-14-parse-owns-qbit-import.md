## goal

Move qBittorrent add/recheck responsibility from autobrr's qBittorrent action into `POST /api/parse` while keeping `/api/pack` as the match gate.

## scope

- add per-client qBittorrent import policy to config
- make `/api/parse` recompute matches instead of depending on prior in-memory state
- add qBittorrent add/recheck/resume orchestration
- update operator docs and webhook setup
- add regression tests for parse-owned import flow

## non-goals

- changing auth behavior
- replacing `/api/pack`
- broad qBittorrent category management features beyond add/recheck/resume

## risks

- hardlink target path drift from qBittorrent save path
- stale/incomplete torrent-state polling after add
- config/docs drift for new client fields
- false positives in release matching still remain safety-critical

## step list

1. Add config/domain/schema surface for qBittorrent import policy.
2. Refactor processor matching into reusable parse/pack path.
3. Implement qBittorrent add/recheck/resume in `/api/parse`.
4. Add regression tests around standalone parse flow and recheck.
5. Update README and product/design docs.
6. Run formatting and verification gates.

## decision log

- 2026-03-14: Use config-owned qBittorrent add policy keyed by `clientname`; keep webhook payload minimal.
- 2026-03-14: Remove `/api/parse` dependence on `matchMap`; recompute matches for standalone correctness.
- 2026-03-14: Initial import flow defaulted qBittorrent add save path to client `preImportPath`; superseded on 2026-03-15 by explicit destination validation that preserves qBittorrent defaults.

## verification notes

- `gofumpt -w internal/config/config.go internal/http/processor.go internal/http/processor_test.go internal/torrents/torrents.go internal/domain/config.go internal/domain/http.go`
- `go fix ./...`
- `go test -v ./...`
- `govulncheck ./...`
