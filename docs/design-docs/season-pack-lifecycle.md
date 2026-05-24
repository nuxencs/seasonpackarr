# Season Pack Lifecycle

## Goal

Describe the normal path from webhook hit to hardlink creation.

## Flow

1. Service starts through `cmd/start.go`.
2. Config, logger, notification sender, and metadata providers are initialized.
3. `internal/http/server.go` exposes authenticated `/api/pack` and `/api/parse`.
4. A webhook request enters `internal/http/webhook.go`.
5. `internal/http/processor.go` decodes the payload and locates the configured client.
6. Existing client episode releases are inspected.
7. `internal/release/` compares announce and client releases under configured fuzzy-matching rules.
8. Smart mode may consult metadata providers to decide whether a grab is worthwhile.
9. `POST /api/parse` decodes torrent contents to derive stable target naming.
10. Matching episode files are hardlinked into the season-pack target directory.
11. `POST /api/parse` resolves the effective qBittorrent destination, hardlinks into that path, imports the season pack using explicit overrides only, rechecks if qBittorrent reports missing files, and resumes the torrent.
12. Logs and notifications communicate outcome.

## Failure Classes

- request decode/auth failure
- client connectivity or login failure
- metadata lookup failure
- release mismatch
- torrent parse mismatch
- filesystem hardlink failure
- qBittorrent import or recheck failure

## Verification Notes

Verified against code on 2026-03-14. The lifecycle is implementation-backed, not aspirational.
