# Webhook Contract

## Endpoints

- `POST /api/pack`
- `POST /api/parse`

## Authentication

Requests must include the configured API token. Unauthorized requests are rejected.

## Behavioral Intent

- `/api/pack` processes a season-pack announce against existing client data
- `/api/parse` recomputes matching against existing client data, uses torrent-content-aware pathing, validates the effective qBittorrent destination against `preImportPath`, hardlinks matching files, then imports the season pack into qBittorrent using the configured client import policy

## Contract Stability Rules

- do not change auth expectations casually
- if payload shape or required fields change, update CLI test helpers and docs in the same diff
- qBittorrent import behavior should be configured per client, not spread across ad-hoc webhook payload fields
- if an endpoint becomes broader or narrower in acceptance, call out the operator impact
