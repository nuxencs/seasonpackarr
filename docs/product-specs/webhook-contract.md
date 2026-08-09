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

## Contract Stability Rules

- do not change auth expectations casually
- if payload shape or required fields change, update CLI test helpers and docs in the same diff
- if an endpoint becomes broader or narrower in acceptance, call out the operator impact
