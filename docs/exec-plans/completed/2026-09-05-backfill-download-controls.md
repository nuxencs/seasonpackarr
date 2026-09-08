# Backfill download controls

## Goal
Limit tracker access and avoid unnecessary torrent metadata downloads.

## Scope
Shared indexer allowlist, search-only dry runs, explicit exact previews, local source checks, bounded metadata cache, API and CLI reports, documentation, and a visual walkthrough.

## Non-goals
Cross-seeding, media download management, persistent cache, tracker UI.

## Risks
Unknown coverage must stay unknown until exact planning. Cached bytes must never bypass current inventory, source, or matching checks. Candidate webhooks must remain summary-only.

## Steps
- [x] Add configuration and preview modes.
- [x] Add source preflight and bounded metadata reuse.
- [x] Verify API, CLI, configuration, and cache behavior.
- [x] Update docs and deliver concrete visual cases.

## Decisions
- Empty indexerIDs means all enabled searchable torrent indexers. Nonempty lists restrict every run; unavailable selections produce failures.
- Dry run returns candidates with null coverage. Verify requires dry run; import always verifies.
- Metadata cache is process-local, seven days, at most 64 MiB and 1024 entries. Connection changes clear it. Cache contains bytes, never matching decisions.

## Verification
Passed:
- `go test -v ./...`.
- `go test -race ./internal/http ./internal/config ./cmd ./internal/release ./internal/prowlarr`.
- Final metadata-key and report changes: `go test -race ./internal/http`.
- CLI smoke test covers search-only preview, exact preview, and import through the authenticated API. Discovery has no file-detail or metadata requests; import reuses preview metadata.
- `govulncheck ./...`: no reachable vulnerabilities. Four module advisories remain outside called code.
- `deadcode ./...`: four existing findings, no new findings.
- `go fix ./...`, `gofumpt -w .`, `go mod tidy -v`: clean; no module changes.
- Browser inspection of concrete cases in dark and light themes at desktop and 320/360-pixel widths. Case selection updates the displayed checks and download count.

Live tracker requests, credentials, mounts, and client imports remain installation-specific checks. No live tracker was queried. The product spec includes the operator checklist.
