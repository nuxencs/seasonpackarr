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
14. The processor returns an explicit success, rejection, or failure outcome. One response module applies the matching log severity, notification payload, and unchanged legacy webhook reason contract.

## Outcome Classes

- Success: the stage completed its match or import operation. It logs at info level and maps to the `MATCH` notification level.
- Rejection: the release does not meet matching or smart-mode policy. It logs at info level without an error field and maps to the `INFO` notification level.
- Failure: request, client, torrent, filesystem, or import work failed. It logs at error level with the cause and maps to the `ERROR` notification level.

The processing stage chooses the outcome kind. The legacy webhook reason code
remains stable. It does not select log or notification severity. For example,
reason `445` is a rejection when inspected files do not match the pack, but it
is a failure when candidate file details cannot be read.

HTTP rejection and failure bodies use the canonical reason text. Operational
causes stay in structured logs and failure notifications. If one or more
hardlink faults make achieved smart-mode coverage insufficient, the result is a
failure at the stable below-threshold reason code, not a policy rejection.

Failures include:

- request decode failure
- client connectivity or login failure
- client inventory or file-detail failure
- torrent decode or parse failure
- filesystem hardlink failure
- client import or verify failure

## Verification Notes

Verified against code on 2026-08-10. The lifecycle is implementation-backed, not aspirational.
