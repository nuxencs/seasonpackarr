# Webhook Contract

## Endpoints

- `POST /api/pack`
- `POST /api/parse`

## Authentication

Requests must include the configured API token. Unauthorized requests are rejected.

## Behavioral Intent

- `/api/pack` processes a season-pack announce against existing client data
- `/api/parse` does the same with torrent-content-aware pathing when torrent bytes or a torrent-derived payload are available

## Contract Stability Rules

- do not change auth expectations casually
- if payload shape or required fields change, update CLI test helpers and docs in the same diff
- if an endpoint becomes broader or narrower in acceptance, call out the operator impact
