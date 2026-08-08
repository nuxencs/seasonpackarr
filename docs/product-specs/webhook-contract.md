# Webhook Contract

## Endpoints

- `POST /api/pack`
- `POST /api/parse`

## Authentication

Requests must include the configured API token. Unauthorized requests are rejected.

## Behavioral Intent

- `/api/pack` is a pure match gate: it decides whether a season-pack announce matches episodes already in the client and returns a successful match; it has no filesystem side effects and adds nothing to the client
- `/api/parse` owns the import: it decodes the torrent payload, recomputes the matches, resolves the client's import destination, hardlinks matched episodes in the client-selected content layout, and imports the torrent into the client via the per-client `import` policy (add stopped, account for present data through the client's check flow, and do not leave the import paused)

## Contract Stability Rules

- do not change auth expectations casually
- if payload shape or required fields change, update CLI test helpers and docs in the same diff
- if an endpoint becomes broader or narrower in acceptance, call out the operator impact
