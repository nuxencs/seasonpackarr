# Deluge client support

## Goal

Add Deluge 1.3 and 2.x daemon support through
`github.com/autobrr/go-deluge` while preserving the existing hardlink and
import guarantees.

## Scope

- add distinct Deluge V1 and V2 adapters behind
  `internal/torrentclient.TorrentClient`
- map the single configured import tag to the Deluge Label plugin
- add Deluge config validation, schema, examples, and operator documentation
- add unit tests for connection construction, torrent mapping, destination
  resolution, and stopped-add/check/start import behavior
- add environment-gated integration tests against real Deluge 1.3.15 and 2.x
  daemons, including label creation and assignment
- update architecture and client import documentation

## Non-goals

- Deluge Web JSON-RPC support
- mapping more than one `import.tags` entry to Deluge

## Risks

- Deluge uses a native TLS RPC daemon, not an HTTP endpoint.
- File paths must remain relative to the torrent save path so hardlink sources
  stay correct.
- Imports must not start before Deluge has checked the hardlinked data.
- The adapter uses Deluge's legacy info-hash torrent ID and therefore rejects
  BitTorrent v2-only torrents.

## Steps

- [x] Inspect issue 72, the existing adapter seam, and `autobrr/go-deluge`.
- [x] Audit Deluge V1, Deluge V2, go-deluge, and Autobrr source behavior.
- [x] Add V1/V2 selection and single-label support with test seams.
- [x] Add environment-gated Deluge V1/V2 integration tests.
- [x] Run both integration suites against real daemons.
- [x] Keep the environment-gated integration tests out of CI for this change.
- [x] Update config validation, schema, examples, and docs.
- [x] Run all repository checks and move this plan to `completed/`.

## Decision log

- 2026-08-08: Expose `deluge-v1` and `deluge-v2` client types because the
  selected module and Autobrr use separate protocol clients. Keep `deluge` as
  an alias for V2 so the earlier configuration remains valid.
- 2026-08-08: Add torrents stopped with `download_location`, apply the optional
  label, resume, then wait until Deluge is no longer paused or checking. This
  uses the upstream module and Deluge/libtorrent's normal initial check.
- 2026-08-08: Map zero or one `import.tags` entry to Deluge's Label plugin.
  Follow Autobrr's disabled-plugin behavior. When enabled, list label
  definitions, create the configured label if absent, then assign it. This
  avoids an intentional `Unknown Label` exception in Deluge's logs. Reject
  multiple labels.
- 2026-08-08: Treat the Deluge V1 empty duplicate result and the Deluge V2
  `AddTorrentError` as no-action results. Return before label assignment,
  resume, or status polling. Normal HTTP duplicate requests stop earlier at
  seasonpackarr's request-level gate.
- 2026-08-08: Use go-deluge v1.4.0 only as a normal Go module dependency. Do
  not copy or extend its source in this repository.
- 2026-08-08: Keep daemon setup and fixture data outside the repository. The
  live test connects to operator-supplied V1 and V2 endpoints through
  environment variables.

## Verification notes

- `TestDelugeImportLive` against Deluge 1.3.15: passed.
- `TestDelugeImportLive` against Deluge 2.1.2: passed.
- Both live suites verified complete and partial initial checks, resume state,
  paths, files, missing-label creation, and label assignment.
- `go test -v ./...`: passed.
- `go build ./...`: passed.
- `go test -race ./internal/torrentclient ./internal/config`: passed.
- `go vet ./...`: passed.
- `deadcode ./...`: reports three pre-existing helpers. They remain unchanged
  because removing them is outside this feature's scope and two are exported.
- `govulncheck ./...`: no reachable vulnerabilities.
- `jq empty schemas/config-schema.json`: passed.
- `git diff --check`: passed.
- Workflow diff: empty. Deluge integration tests remain local only.
- go-deluge is a standard v1.4.0 module dependency. No copied dependency
  source remains.
