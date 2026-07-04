# Season Pack Lifecycle

## Goal

Describe the normal path from webhook hit to hardlink creation and client import.

## Flow

1. Service starts through `cmd/start.go`.
2. Config, logger, notification sender, and metadata providers are initialized.
3. `internal/http/server.go` exposes authenticated `/api/pack` and `/api/parse`.
4. A webhook request enters `internal/http/webhook.go`.
5. `internal/http/processor.go` decodes the payload and locates the configured client.
6. Existing client episode releases are inspected.
7. `internal/release/` compares announce and client releases under configured fuzzy-matching rules.
8. Smart mode may consult metadata providers to decide whether a grab is worthwhile.
9. `/api/pack` stops here: it only reports whether the pack matches. `/api/parse` continues by decoding the torrent
   contents to derive stable target naming.
10. The client's import root is resolved, matches are recomputed, and matching episode files are hardlinked into the
    season-pack directory beneath the import root.
11. The torrent is added to the client, verified/rechecked so the present files are recognised, and resumed (unless
    configured to stay paused). See [qbittorrent-import-flow.md](qbittorrent-import-flow.md) for the complete-vs-partial
    client import flow with diagrams.
12. Logs and notifications communicate outcome.

## Failure Classes

- request decode/auth failure
- client connectivity or login failure
- metadata lookup failure
- release mismatch
- torrent parse mismatch
- filesystem hardlink failure
- client import or verify failure

## Verification Notes

Verified against code on 2026-07-04. The lifecycle is implementation-backed, not aspirational.
