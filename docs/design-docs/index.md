# Design Docs Index

Design docs capture stable reasoning about how this system should work.

## Catalog

| Doc | Purpose | Verification Status | Last Reviewed |
| --- | --- | --- | --- |
| `core-beliefs.md` | Agent-first operating beliefs for this repo | Verified against repo structure and workflows | 2026-03-14 |
| `season-pack-lifecycle.md` | End-to-end processing model from webhook to hardlink | Verified against `cmd/start.go`, `internal/http/*`, `internal/release/*` | 2026-03-14 |
| `matching-and-hardlinking.md` | Safety and correctness constraints for matching and file linking | Verified against `internal/release/*`, `internal/files/*` | 2026-03-14 |
| `qbittorrent-import-flow.md` | `/api/parse` client import flow (complete vs partial pack) with diagrams | Verified against `internal/torrentclient/*` and live qBittorrent 5.x / Transmission | 2026-07-04 |

## Usage

- read `core-beliefs.md` first
- read one narrow design note before editing a risky subsystem
- if you discover design drift, update the doc in the same change or log it in `../exec-plans/tech-debt-tracker.md`
