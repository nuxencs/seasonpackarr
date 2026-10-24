# AGENTS.md

seasonpackarr is a Go service and CLI for TV automation with autobrr and Prowlarr. It hardlinks reusable episodes into a season pack and imports the pack, so only missing data downloads. Use the terms in `GLOSSARY.md`.

## Where to look

- Packages, runtime flow, layering: `ARCHITECTURE.md`
- Checks, code and docs sync rules, security rules: `docs/DEVELOPMENT.md`
- Test layers, test names, harness: `docs/TESTING.md`. Read it before you add or rename a test.
- Matching, hardlinks, or client import changes: read the note in `docs/design/` first.
- User-visible behavior: the guides that `README.md` links. The API contract is `docs/API.md`.
- Decisions: `docs/adr/`

## Invariants

- `/api/candidate`, `/api/match`, `/api/import`, and `/api/search` stay authenticated behind `apiToken`.
- Matching changes keep the core promise: prevent unnecessary downloads without broadening false positives.
- The candidate check requests no torrent bytes and no per-torrent file details.
- The match has no side effects. The import owns all filesystem and client changes.
- Smart-mode coverage counts distinct valid torrent episode targets and never exceeds 100 percent.
- Hardlink path construction is safety-critical. Treat target paths and pre-import directory assumptions as high risk, and add regression tests.

## Verification

Run the checks from the table in `docs/DEVELOPMENT.md#checks` for the code you changed. Before you claim that a change to the request flow, planning, hardlinks, client adapters, config loading, or the image works, run `test/harness/run.sh e2e`.

## Keep in sync

- Config: `config.yaml`, `schemas/config-schema.json`, and `docs/CONFIGURATION.md` change together.
- API: payload, status code, or endpoint changes update `docs/API.md`.
- autobrr filter: `docs/AUTOBRR.md` and `test/e2e/testdata/filter.json` change together.
- User setup or upgrade steps: update the guide in `docs/` in the same change.

## Agent skills

### Issue tracker

GitHub Issues on `nuxencs/seasonpackarr`, through the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Canonical triage roles map to the repo's `Status: ...` labels. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: root `GLOSSARY.md` and `docs/adr/`. See `docs/agents/domain.md`.
