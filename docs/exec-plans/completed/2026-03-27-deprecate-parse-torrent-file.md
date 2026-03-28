## goal

Fail fast when deprecated `parseTorrentFile` config or env input is still present.

## scope

- detect stale `parseTorrentFile` in YAML config before normal validation
- detect stale `SEASONPACKARR__PARSE_TORRENT_FILE` env usage
- hard-fail startup with a deprecation message
- apply the same safeguard during config reload
- add focused config tests

## non-goals

- reintroducing `parseTorrentFile` to runtime config structs
- warning-only compatibility mode
- broader unknown-key validation for all config fields

## risks

- stale operator env/config causing startup failure after upgrade
- reload callback terminating the process when deprecated input appears
- test surface drifting if config load paths change

## step list

1. Add deprecated-input validation helper in config loader.
2. Run helper before env parsing/client validation in startup flow.
3. Run helper in dynamic reload before unmarshal/apply.
4. Add config tests for YAML reject, env reject, clean config pass.
5. Run formatting and verification.

## decision log

- 2026-03-27: treat deprecated key presence as invalid even when value is `false`.
- 2026-03-27: deprecation message should be date-free and explicit that parsing is always enabled.

## verification notes

- `gofumpt -w internal/config/config.go internal/config/config_test.go`
- `go fix ./...`
- `go test -v ./...`
- `govulncheck ./...`
