# Webhook Contract

## Endpoints

- `POST /api/candidate`
- `POST /api/match`
- `POST /api/import`

`/api/match` replaces `/api/pack`, and `/api/import` replaces `/api/parse`.
The old routes return `404`; there are no aliases or redirects. The CLI uses
`test match` and `test import`; `test pack` and `test parse` are removed.
Authentication, payloads, status codes, and processing behavior are unchanged
for the renamed operations. Update callers when upgrading the service.

## Authentication

Requests must include the configured API token. Unauthorized requests are rejected.

## Behavioral Intent

- `/api/candidate` is an announce-only match gate. It reads torrent summaries but never requests torrent bytes or per-torrent file details.
- `/api/match` requires torrent bytes. It counts MKV and MP4 files that parse as episodes, excludes samples and extra videos, maps reusable client files to distinct same-container targets, applies smart-mode coverage, and caches an accepted import plan. It has no filesystem or client side effects.
- `/api/import` owns the import. It reuses the accepted plan when available and safely rebuilds it on a cache miss. If a planned source disappears during hardlinking, it refreshes the client inventory and exact plan once, then retries the idempotent hardlink set. It resolves the client destination, hardlinks matched episodes, and imports through the per-client policy. If achieved smart-mode coverage falls below the threshold, it does not import. Successful hardlinks remain for a safe retry; the service does not perform destructive cleanup.
- The candidate and match checks share a short-lived client inventory. A normal candidate, match, import sequence performs one inventory scan and one set of candidate file-detail reads.

## Operator Log Contract

- Match and import notifications use the action labels `Match` and `Import`.
- `/api/candidate` logs each release compatibility rejection as a structured
  event. The event reports the incompatible field, the original announced
  `want` value, the original client `got` value, and the client release. Fuzzy
  matching can normalize values for comparison but not for diagnostics.
- When `/api/candidate` passes, it logs each matching client release at `DEBUG`.
- `/api/match` logs the reusable, unmatched, and total torrent episode counts.
- Each unmatched torrent episode produces one structured log event. The event
  reports either a missing source episode, a duplicate torrent target, or the
  closest same-episode client source plus every incompatible safety field. Each
  incompatible field reports its torrent `want` and client `got` value.
- `/api/import` logs whether it reused or rebuilt the import plan. It also logs
  each successful or failed destination-resolution attempt at `INFO`, hardlink
  duration, each torrent-client import stage, total client-import duration, and
  total import duration. A missing hardlink source logs a static warning with a
  structured `source` path, followed by the bounded inventory and plan refresh
  result.
- Torrent-client stages use the stable names `config`, `add`, `find`,
  `recheck`, and `resume`. A client omits stages that it does not perform.
- Logs never include configured credentials or request authentication tokens.
- Expected candidate and match gate rejections use informational events.
  Configuration, decoding, client, filesystem, and import failures use error
  events.

## Contract Stability Rules

- do not change auth expectations casually
- if payload shape or required fields change, update CLI test helpers and docs in the same diff
- if an endpoint becomes broader or narrower in acceptance, call out the operator impact

## Backfill Coordination

`POST /api/search` uses the same authentication middleware. Its separate request
and response contract is documented in [Prowlarr backfill](prowlarr-backfill.md).

Webhook and backfill imports to the same configured client endpoint are
serialized. Import rechecks the candidate gate before it uses a cached exact
plan. A prior import can therefore cause a previously matched request to return
`210` (already in client). Every attempted client import invalidates endpoint
inventories and plans, including failed attempts that may already have added the
pack. A retry refreshes client state before it creates another import.
