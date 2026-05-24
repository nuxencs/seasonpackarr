## goal

Remove `preImportPath` from the user config and derive the hardlink root from qBittorrent's final destination.

## scope

- remove the config/schema/env/docs surface for `preImportPath`
- reject existing `preImportPath` config/env usage as deprecated
- resolve hardlink targets from `qbit.savePath`, category save path, or default save path
- keep `qbit.downloadPath` as qBittorrent temporary download path only
- update regression coverage

## non-goals

- changing webhook payloads
- changing qBittorrent add/recheck/resume behavior
- using `qbit.downloadPath` as a final destination

## risks

- hardlink target drift if qBittorrent destination resolution is wrong
- stale docs/schema making existing users configure removed fields
- confusing `downloadPath` with final save path

## step list

1. Remove `preImportPath` from config and generated samples.
2. Add deprecated config/env rejection.
3. Resolve import root from qBittorrent destination in `/api/parse`.
4. Update docs/schema/references.
5. Update processor/config tests.
6. Run formatting and verification.

## decision log

- 2026-05-24: hard remove `preImportPath`, matching the earlier `parseTorrentFile` removal style.
- 2026-05-24: `qbit.downloadPath` remains temporary/incomplete only.

## verification notes

- `gofumpt -w .`
- `go fix ./...`
- `go mod tidy -v`
- `go test -v ./...`
- `govulncheck ./...`
- `deadcode ./...` reported existing unreachable funcs: `internal/http/health.go:37 writeUnhealthy`, `pkg/errors/errors.go:34 Sentinel`, `pkg/errors/errors.go:128 RecoverPanic`
