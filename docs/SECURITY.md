# SECURITY.md

## Security Posture

This is a self-hosted integration service with network-facing HTTP endpoints and local filesystem effects. Main risks are unauthorized API use, unsafe path behavior, credential leakage, and supply-chain drift.

## Current Controls

- API token auth for `/api/pack` and `/api/parse`
- explicit config-driven client credentials
- narrow HTTP surface
- CodeQL in CI
- Go vulnerability scanning expected in local verification

## Known Dependency Risk

Verified on 2026-03-14 with `govulncheck ./...`:

- `github.com/go-viper/mapstructure/v2@v2.2.1` is flagged by `GO-2025-3900`
- `github.com/go-viper/mapstructure/v2@v2.2.1` is flagged by `GO-2025-3787`

Both traces reach config loading through `koanf.Unmarshal` in `internal/config/config.go`.

## High-Risk Areas

- path construction before hardlink creation
- logging of sensitive config or tokens
- webhook contract drift causing unexpected processing
- external API dependencies and release-parser assumptions

## Rules For Changes

- never log API tokens, passwords, or raw secrets
- treat filesystem target-path derivation as security-sensitive
- document any new outbound network dependency in `docs/references/`
- add verification notes when auth, pathing, or external request logic changes

## Current Gap

There is no dedicated secret-redaction test suite and no doc linter ensuring security docs stay synced with behavior.
