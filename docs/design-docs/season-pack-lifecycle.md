# Season Pack Lifecycle

## Goal

Describe the normal path from webhook hit to hardlink creation and client import.

## Flow

1. Service starts through `cmd/start.go`.
2. Config, logger, and notification sender are initialized.
3. `internal/http/server.go` exposes authenticated `/api/candidate`, `/api/pack`, and `/api/parse`.
4. A webhook request enters `internal/http/webhook.go`.
5. `internal/http/processor_handlers.go` decodes the payload. The candidate, plan, and import processor files keep the three lifecycle stages separate.
6. `/api/candidate` compares the announce with cached torrent summaries. It does not read torrent bytes or file details.
7. `/api/pack` reuses the short-lived inventory, parses the announced torrent, and requests details only for candidate client torrents.
8. `internal/release/` keeps MKV and MP4 files that parse as episodes. It excludes samples, extra videos, and other non-episode files from coverage.
9. Reusable source files map to distinct valid targets with the same container under configured fuzzy rules.
10. Smart mode compares the exact reusable target count with the actual torrent episode count. `/api/pack` caches an accepted side-effect-free plan.
11. `/api/parse` reuses that plan, or safely rebuilds it after a cache miss. It resolves the import destination and hardlinks matching files in the client-selected layout.
12. Smart mode checks achieved coverage after hardlink creation. If it is below the threshold, the request does not import the torrent. Successful hardlinks remain for a safe retry. Automatic cleanup is intentionally deferred because the service cannot yet prove ownership of every file and directory it would remove.
13. The torrent is added to the client, checked so the present files are recognised, and not left paused. See
    [qbittorrent-import-flow.md](qbittorrent-import-flow.md) for the complete-vs-partial
    client import flow with diagrams.
14. Logs and notifications communicate outcome.

## Failure Classes

- request decode/auth failure
- client connectivity or login failure
- client inventory or file-detail failure
- release mismatch
- torrent parse mismatch
- filesystem hardlink failure
- client import or verify failure

## Verification Notes

Verified against code on 2026-08-09. The lifecycle is implementation-backed, not aspirational.
