# AGENTS.md

## Purpose

This repo should be easy for humans and coding agents to change safely.

Start small. Read the stable entrypoints first:

1. `README.md`
2. `ARCHITECTURE.md`
3. `docs/PLANS.md`
4. `docs/QUALITY_SCORE.md`
5. the most relevant index under `docs/`

Use progressive disclosure. Do not read the whole repo up front unless the task forces it.

## Product In One Paragraph

`seasonpackarr` is a Go service and CLI helper for autobrr-driven TV automation. It receives authenticated webhook calls for season packs, compares announced releases against already-downloaded episode releases, optionally uses torrent parsing plus metadata providers to reduce false matches, and hardlinks matching episode files into the season-pack folder so existing data is reused instead of downloaded again.

## Source Of Truth

- Product overview and user setup: `README.md`
- Top-level domain/package map: `ARCHITECTURE.md`
- Design beliefs and lifecycle docs: `docs/design-docs/index.md`
- Product intent and user flows: `docs/product-specs/index.md`
- Planning rules and execution plan storage: `docs/PLANS.md`
- Quality/risk posture: `docs/QUALITY_SCORE.md`, `docs/RELIABILITY.md`, `docs/SECURITY.md`
- Generated durable surface notes: `docs/generated/db-schema.md`
- Compact agent-oriented references: `docs/references/`

## Repo Map

- `cmd/`: CLI entrypoints for `start`, `test`, `parse`, `pack`, version/token helpers
- `internal/http/`: API server, auth, health, webhook handlers, processing orchestration
- `internal/release/`: release matching logic and season-pack comparisons
- `internal/torrents/`: torrent fetch/decode helpers
- `internal/metadata/`: TVDB/TVMaze episode-count providers
- `internal/files/`: hardlink creation
- `internal/config/`: config defaults, loading, reload, schema-adjacent behavior
- `internal/domain/`: shared domain structs and status codes
- `internal/notification/`: Discord notifications
- `schemas/`: JSON schema for config surface
- `distrib/`, `Dockerfile`, `ci.Dockerfile`: runtime packaging

## Agent Operating Loop

1. Read the smallest relevant doc entrypoint first.
2. Inspect the concrete package/files that implement the behavior.
3. State assumptions before larger edits.
4. Prefer root-cause fixes over heuristics.
5. Keep docs and code in sync in the same change.
6. Add or update regression tests when matcher, parsing, pathing, or config behavior changes.
7. Verify with the strongest checks available before handoff.

## Core Invariants

- `/api/pack` and `/api/parse` stay authenticated behind `APIToken`.
- Matching changes must preserve the core promise: prevent unnecessary redownloads without silently broadening false positives.
- Config changes must update `config.yaml`, `schemas/config-schema.json`, and relevant docs together.
- Hardlink path behavior is safety-critical. Treat path construction and pre-import directory assumptions as high risk.
- Metadata-provider fallbacks should fail loudly in logs and degrade predictably.
- If a change alters webhook payload expectations or CLI testing flow, update product specs and references in the same diff.

## Verification

Primary local checks:

- `go test -v ./...`
- `govulncheck ./...`
- `gofumpt -w .` when Go files change
- focused CLI/API smoke checks when behavior touches request flow:
  - `go run . start --config <dir>`
  - `go run . test pack "<release>" --client "<name>" --host 127.0.0.1 --port 42069 --api "<token>"`
  - `go run . test parse "<release-or-torrent>" --client "<name>" --host 127.0.0.1 --port 42069 --api "<token>"`

CI currently enforces:

- `go test -v ./...`
- release builds
- Docker builds
- CodeQL

## Plans As Artifacts

Small work: keep a lightweight plan in the task thread.

Complex or multi-step work: create or update a checked-in plan under `docs/exec-plans/active/`. Move it to `docs/exec-plans/completed/` when done. Track persistent gaps in `docs/exec-plans/tech-debt-tracker.md`.

## Knowledge Base Maintenance

When code changes invalidate docs, fix docs now. Do not leave stale architecture or behavior notes behind.

If docs are missing:

- add the smallest document that makes the next change safer
- link it from the nearest index
- record verification status if the doc describes behavior

This repo prefers short, navigable docs over a giant manual.
