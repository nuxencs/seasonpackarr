# Design Docs Index

Design docs capture stable reasoning about how this system should work.

## Catalog

| Doc | Purpose | Verification Status | Last Reviewed |
| --- | --- | --- | --- |
| `core-beliefs.md` | Agent-first operating beliefs for this repo | Verified against repo structure and workflows | 2026-08-09 |
| `season-pack-lifecycle.md` | End-to-end processing and operator log model from webhook to import | Verified against `cmd/start.go`, `internal/http/*`, `internal/release/*` | 2026-08-12 |
| `matching-and-hardlinking.md` | Safety, diagnostics, and correctness constraints for matching and file linking | Verified against `internal/release/*`, `internal/files/*` | 2026-08-12 |
| `qbittorrent-import-flow.md` | `/api/import` client import flow, timings, and complete-vs-partial behavior | Verified against `internal/torrentclient/*`, real-daemon timings (2026-10-07), and the integration harness entries | 2026-10-09 |

## Usage

- read `core-beliefs.md` first
- read one narrow design note before editing a risky subsystem
- if you discover design drift, update the doc in the same change or log it in `../exec-plans/tech-debt-tracker.md`
