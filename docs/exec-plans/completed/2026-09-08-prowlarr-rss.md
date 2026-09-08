# Prowlarr RSS Monitoring

## Goal

Run season-pack discovery without autobrr. RSS is the only automatic discovery
mechanism. Targeted historical searches remain manual; autobrr remains optional.

## Scope

- Replace the targeted-search interval with an opt-in RSS interval.
- Poll selected RSS-capable torrent indexers through Prowlarr.
- Reuse existing release, source, coverage, duplicate, and import checks.
- Share request spacing, cooldowns, and torrent metadata between discovery modes.
- Keep RSS periodic only and retain bounded candidates for later coverage checks.
- Update config, schema, API/CLI docs, and regression coverage.

## Non-goals

- Historical RSS guarantees, disk queues, or a second automatic search schedule.
- Episode acquisition, quality ranking, parser changes, or server deployment.

## Risks

- Feed windows can miss releases after downtime; manual search covers those gaps.
- Seen releases can become usable later. Do not cache rejection decisions.
- RSS and manual searches must not bypass shared rate limits or duplicate checks.

## Steps

1. [x] Inspect existing flow and local Prowlarr source contracts.
2. [x] Add RSS adapter, shared connection lifetime, and bounded feed state.
3. [x] Replace scheduling config and keep the CLI/API limited to targeted searches.
4. [x] Cover feed paging, reevaluation, duplicate checks, cooldowns, and scheduling.
5. [x] Update docs and run affected checks.

## Decision Log

- `search.rssInterval` defaults to `0s`, with a positive minimum of 10 minutes.
  `search.interval` and its environment override are removed.
- First poll reads one recent page per indexer. Later polls can page back to an
  overlap with the previous poll, with a ten-page limit and explicit gap reports.
- Relevant season-pack candidates remain in bounded process memory for seven
  days and are checked against current inventory on each poll. No durable state.
- Runs with incomplete client inventory do not advance the automatic feed checkpoint. A regression test reproduced
  lost releases after client recovery before the incomplete-inventory guard.

- Query groups use normalized title and season only. Different or missing years
  share tracker requests; local release and duplicate checks still compare years
  unless `skipYearCompare` is enabled.

## Verification Notes

- `go test -race ./...` passed. After the client-recovery fix,
  `go test -race ./internal/http` passed again, including the regression and CLI smoke flows.
- `go vet` passed for affected packages. `gofumpt`, `git diff --check`,
  `go fix -diff`, and `go mod tidy -diff` were clean. The config schema is valid JSON.
- Linux amd64 build passed with `CGO_ENABLED=0`.
- `govulncheck ./...`: no reachable or imported-package vulnerabilities; four
  uncalled module advisories. `deadcode ./...`: the same four existing findings,
  with no new unreachable production code.
- HTTP fixtures cover RSS-only indexers, paging and gaps, partial failures,
  client recovery, coverage reevaluation, cross-tracker duplicates, shared
  cooldowns, and scheduler cancellation. CLI tests exercise targeted search previews and imports.
- Live Prowlarr/tracker RSS behavior was not tested. No Hades changes were made.
  Prowlarr source was inspected at the revision recorded in the API audit.
- The scheduler is the only RSS entrypoint. Manual search has no RSS selector.
  After this change, affected HTTP/CLI race tests and vet passed.
- Work stays on `feat/prowlarr-backfill`, as requested. No follow-up branch or PR.
