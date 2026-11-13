# SECURITY.md

## Security Posture

This is a self-hosted integration service with network-facing HTTP endpoints and local filesystem effects. Main risks are unauthorized API use, unsafe path behavior, credential leakage, and supply-chain drift.

## Current Controls

- API token auth for `/api/candidate`, `/api/match`, and `/api/import`
- explicit config-driven client credentials, including qBittorrent passwords or API keys
- narrow HTTP surface
- CodeQL in CI
- Go vulnerability scanning expected in local verification

## Known Dependency Risk

Verified on 2026-08-09 with `govulncheck -show verbose ./...`: no reachable symbol or imported-package vulnerabilities.
The scan reports `GO-2026-5932` in the required `golang.org/x/crypto` module, but seasonpackarr does not import the
affected `openpgp` package. That advisory has no fixed module version.

## High-Risk Areas

- path construction before hardlink creation
- logging of sensitive config or tokens
- webhook contract drift causing unexpected processing
- torrent-client dependencies and release-parser assumptions

## Rules For Changes

- never log API tokens, API keys, passwords, or raw secrets
- treat filesystem target-path derivation as security-sensitive
- document any new outbound network dependency in `docs/references/`
- add verification notes when auth, pathing, or external request logic changes

## Current Gap

- no dedicated secret-redaction test suite
- no containment or symlink-escape enforcement for torrent-derived hardlink targets
- no adversarial traversal, absolute-path, or symlink tests
- no doc linter ensuring security docs stay synced with behavior
