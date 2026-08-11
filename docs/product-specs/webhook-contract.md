# Webhook Contract

## Endpoints

- `POST /api/candidate`
- `POST /api/pack`
- `POST /api/parse`

## Authentication

Requests must include the configured API token. Unauthorized requests are rejected.

## Behavioral Intent

- `/api/candidate` is an announce-only match gate. It reads torrent summaries but never requests torrent bytes or per-torrent file details.
- `/api/pack` requires torrent bytes. It counts MKV and MP4 files that parse as episodes, excludes samples and extra videos, maps reusable client files to distinct same-container targets, applies smart-mode coverage, and caches an accepted import plan. It has no filesystem or client side effects.
- `/api/parse` owns the import. It reuses the accepted plan when available and safely rebuilds it on a cache miss. It resolves the client destination, hardlinks matched episodes, and imports through the per-client policy. If achieved smart-mode coverage falls below the threshold, it does not import. Successful hardlinks remain for a safe retry; the service does not perform destructive cleanup.
- The candidate and pack checks share a short-lived client inventory. A normal candidate, pack, parse sequence performs one inventory scan and one set of candidate file-detail reads.

## Processing Outcomes

Each processing stage returns one of three outcomes without changing the
existing legacy webhook reason-code contract:

- Success returns the successful match reason and logs at info level.
- Rejection returns its filter reason and JSON error body, but logs at info level without an error field. Examples include no matching client release, no exact reusable torrent target, and smart-mode coverage below the configured threshold.
- Failure returns its failure reason and canonical JSON error body, logs at error level, and retains the underlying cause for notifications when available.

The stage selects the outcome kind from context. A legacy webhook reason code
can appear with more than one kind. For compatibility, its numeric value is
still written to the HTTP status line. It is not a standards-based HTTP
classification. Pack and parse notifications keep the same reason codes and
configuration values: success uses `MATCH`, rejection uses `INFO`, and failure
uses `ERROR`. Only failures include a cause. The HTTP error field always uses
the canonical reason text and does not expose client, torrent, or filesystem
implementation details.

If hardlink faults prevent achieved smart-mode coverage from meeting the
threshold, `/api/parse` keeps the below-threshold reason code but classifies the
outcome as a failure. Coverage below the threshold is a rejection only when no
operational fault caused the shortfall.

## Contract Stability Rules

- do not change auth expectations casually
- if payload shape or required fields change, update CLI test helpers and docs in the same diff
- if an endpoint becomes broader or narrower in acceptance, call out the operator impact
