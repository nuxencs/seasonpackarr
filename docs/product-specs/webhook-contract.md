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

Each processing stage returns one of three domain outcomes:

- Success: the operation completed.
- Rejection: the release did not meet matching or smart-mode policy.
- Failure: an invalid request or an operational fault prevented completion.

Every outcome has a semantic `reason`. A failure also has a fault class and an
underlying cause. Reasons do not select HTTP status, log severity, or
notification severity.

The HTTP adapter uses only the outcome kind and failure class:

| Outcome | Fault class | HTTP status |
| --- | --- | --- |
| Success | none | `200 OK` |
| Rejection | none | `422 Unprocessable Entity` |
| Failure | request | `400 Bad Request` |
| Failure | internal | `500 Internal Server Error` |
| Failure | dependency | `502 Bad Gateway` |

Authentication remains separate and returns `401 Unauthorized` when the API
token is missing or invalid.

All valid processing outcomes use one JSON response shape:

```json
{
  "outcome": "rejection",
  "reason": "torrent_mismatch",
  "message": "could not match episodes to files in pack"
}
```

The response does not expose client, torrent, or filesystem implementation
details. Operational causes stay in structured logs and failure notifications.
Invalid domain outcomes fail safely with `500`, `internal_error`, and no
notification.

Client inventory, file inspection, destination discovery, add, recheck, and
resume faults are dependency failures. Invalid local import policy is an
internal failure. A torrent format that the selected client adapter cannot
import is a request failure.

Notification configuration does not change. Success uses `MATCH`, rejection
uses `INFO`, and failure uses `ERROR`. Notification filtering and color depend
only on the outcome kind. The semantic reason supplies the title. Only failure
notifications include the underlying cause.

If hardlink faults prevent achieved smart-mode coverage from meeting the
threshold, `/api/parse` returns an internal failure with reason
`hardlink_failed`. Coverage below the threshold is a normal rejection with
reason `below_threshold` only when no operational fault caused the shortfall.

## Contract Stability Rules

- do not change auth expectations casually
- if payload shape or required fields change, update CLI test helpers and docs in the same diff
- if an endpoint becomes broader or narrower in acceptance, call out the operator impact
