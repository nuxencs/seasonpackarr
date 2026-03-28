## goal

Make qBittorrent add options path-safe while preserving qBittorrent defaults unless the operator explicitly overrides them.

## scope

- remove `auto` as a `qbit.contentLayout` mode
- require `qbit.category` or `qbit.savePath`
- stop inferring qBittorrent save path from `preImportPath`
- validate the effective qBittorrent destination against `preImportPath`
- update docs, schema, samples, and regression tests

## non-goals

- changing webhook payloads
- changing `/api/pack` gate behavior

## risks

- category/default save path mismatch causing silent redownloads
- config/doc drift around optional content layout
- qBittorrent API lookup failures during parse

## step list

1. Update config/schema/docs surface.
2. Extend qBittorrent client abstraction with category/default save-path reads.
3. Validate effective destination before hardlink/import.
4. Adjust add-option builder to omit unset savePath/contentLayout.
5. Add/adjust regression tests.
6. Run formatting and verification.

## decision log

- 2026-03-15: `preImportPath` stays the hardlink target; qBittorrent destination must resolve to the same path.
- 2026-03-15: category-only mode is allowed, but seasonpackarr must verify the category/default qBittorrent path matches `preImportPath`.

## verification notes

- `gofumpt -w internal/config/config.go internal/http/processor.go internal/http/processor_test.go`
- `go fix ./...`
- `go test -v ./...`
- `govulncheck ./...`
