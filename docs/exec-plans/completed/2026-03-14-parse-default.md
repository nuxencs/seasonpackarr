## goal

Make torrent parsing the only supported season-pack flow by removing `parseTorrentFile` as an optional config switch.

## scope

- remove `parseTorrentFile` from config/domain/schema/docs
- make `/api/pack` a pure match gate with no hardlink side effects
- add regression coverage for gate-only `/api/pack`

## non-goals

- changing `/api/parse` payload shape
- changing auth or CLI endpoint names

## risks

- stale docs still describing parse as optional
- unexpected operator reliance on old `/api/pack` hardlink behavior

## step list

1. Remove config/runtime references to `parseTorrentFile`.
2. Make `/api/pack` always return match-only success.
3. Update README/spec docs to describe parse as mandatory.
4. Add regression test for pack gate-only behavior.
5. Run formatting and verification.

## decision log

- 2026-03-14: Treat `/api/parse` as the only supported hardlink/import path; `/api/pack` remains the acceptance gate.

## verification notes

- `gofumpt -w internal/config/config.go internal/domain/config.go internal/http/processor.go internal/http/processor_test.go`
- `go test -v ./...`
- `govulncheck ./...`
