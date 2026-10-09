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

`seasonpackarr` is a Go service and CLI helper for autobrr-driven TV automation. It uses a cheap announce-only candidate check, then builds an exact plan from the torrent and already-downloaded episodes. It hardlinks reusable episode files into the season-pack folder and imports the pack so only missing data downloads.

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

- `cmd/`: CLI entrypoints for `start`, `candidate`, `match`, `import`, `search`, version/token helpers
- `internal/http/`: API server, auth, health, webhook handlers, processing orchestration
- `internal/release/`: release matching logic and season-pack comparisons
- `internal/torrents/`: torrent fetch/decode helpers
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

- `/api/candidate`, `/api/match`, and `/api/import` stay authenticated behind `APIToken`.
- Matching changes must preserve the core promise: prevent unnecessary redownloads without silently broadening false positives.
- `/api/candidate` must not request torrent bytes or per-torrent file details.
- `/api/match` stays side-effect free. `/api/import` owns filesystem and client mutations.
- Smart-mode coverage counts distinct valid torrent episode targets and cannot exceed 100 percent.
- Config changes must update `config.yaml`, `schemas/config-schema.json`, and relevant docs together.
- Hardlink path behavior is safety-critical. Treat path construction and pre-import directory assumptions as high risk.
- If a change alters webhook payload expectations or CLI testing flow, update product specs and references in the same diff.

## Verification

Primary local checks:

- `go test -v ./...`
- `govulncheck ./...`
- `gofumpt -w .` when Go files change
- `internal/torrentclient/testdata/harness/run.sh` when torrent client adapter code changes. It runs the
  integration suites against real daemons in Docker Compose. Pass entry names, for example `qbit-4.3.9`, to run
  one client version, `all` for the full version range, or `list [all]` to print the entries.
- focused CLI/API smoke checks when behavior touches request flow:
  - `go run . start --config <dir>`
  - `go run . candidate "<release>" --config <dir> --client "<name>"`
  - `go run . match "<file.torrent>" --config <dir> --client "<name>"`
  - `go run . import "<file.torrent>" --config <dir> --client "<name>"`

CI currently enforces:

- `go test -v ./...`
- release builds
- Docker builds
- CodeQL
- integration tests in `.github/workflows/integration.yml`: one job for each entry of `run.sh list`, when torrent
  client code or its dependencies change (the workflow lists the paths). A manual run with `full` uses
  `run.sh list all`. It is not a required check.

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

## Agent skills

### Issue tracker

GitHub Issues on `nuxencs/seasonpackarr`, through the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Canonical triage roles map to the repo's `Status: ...` labels. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: root `GLOSSARY.md` and `docs/adr/`. See `docs/agents/domain.md`.
