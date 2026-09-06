# Backfill interval minimums

## Goal

Reduce Prowlarr request frequency and enforce a longer scheduled search interval.

## Scope

Config validation, defaults, schema descriptions, operator docs, and config tests.

## Non-goals

Parser changes, per-group search history, server deployment, and live searches.

## Risks

Existing configs with request spacing below 10s or enabled schedules below 1h
will fail validation. Invalid reloads must preserve the last valid snapshot.

## Steps

1. Done: checked cross-seed and qui guidance, linked from the product spec.
2. Done: enforced the requested minimums and updated config surfaces.
3. Done: verified boundaries, environment overrides, reload rejection, and affected packages.

## Decision log

- Keep scheduling opt-in with 0s. Require at least 1h when enabled.
- Default request spacing to 10s and reject smaller values, including 0s.
- Keep 24h as the schedule example. The requested limits differ from cross-seed
  and qui because those projects also use bulk-search and per-torrent cooldowns.
- Enforce user settings at config validation. Internal HTTP fixtures can retain
  zero-delay clients for fast, isolated tests.

## Verification notes

- Passed: `go test ./internal/config ./internal/http ./internal/prowlarr ./cmd`.
- Config tests cover exact limits, equivalent duration units, values just below
  each limit, previous defaults, disabled schedules, file settings, environment
  overrides, and preservation of the active config after an invalid reload.
- Formatted changed Go files with gofumpt. `git diff --check` and JSON schema
  syntax validation passed.
