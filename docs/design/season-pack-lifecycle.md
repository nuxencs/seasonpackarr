# Season Pack Lifecycle

## Goal

Describe the normal path from webhook hit to hardlink creation and client import.

## Flow

1. Service starts through `cmd/start.go`. `signal.NotifyContext` converts termination signals into cancellation and starts a 15-second graceful shutdown.
2. Config, logger, notification sender, and SQLite discovery state are initialized. The command closes the database after server shutdown.
3. `internal/http/server.go` exposes authenticated `/api/candidate`, `/api/match`, and `/api/import`.
4. A webhook request enters `internal/http/webhook.go`. Its context is passed through planning and torrent-client operations, so client disconnects and request cancellation stop context-aware work.
5. `internal/http/processor_handlers.go` decodes the payload. The candidate, plan, and import processor files keep the three lifecycle stages separate.
6. `/api/candidate` compares the announce with cached torrent summaries. It does not read torrent bytes or file details. At debug level, it logs each client release that caused the gate to pass.
7. `/api/match` reuses the short-lived inventory, parses the announced torrent, and requests details only for candidate client torrents.
8. `internal/release/` keeps MKV and MP4 files that parse as episodes. It excludes samples, extra videos, and other non-episode files from coverage.
9. Reusable source files map to distinct valid targets with the same container under configured fuzzy rules.
10. Smart mode compares the exact reusable target count with the actual torrent episode count. `/api/match` caches an accepted side-effect-free plan.
11. `/api/import` reuses that plan, or safely rebuilds it after a cache miss. It resolves the import destination and hardlinks matching files in the client-selected layout. If a planned source disappeared because the client moved it, the processor invalidates both caches, rebuilds the exact plan, and retries the idempotent links once.
12. Smart mode checks achieved coverage after hardlink creation. If it is below the threshold, the request does not import the torrent. Successful hardlinks remain for a safe retry. Automatic cleanup is intentionally deferred because the service cannot yet prove ownership of every file and directory it would remove.
13. The torrent is added to the client, checked so the present files are recognised, and not left paused. See
    [qbittorrent-import-flow.md](qbittorrent-import-flow.md) for the complete-vs-partial
    client import flow with diagrams.
14. Pack logs explain unmatched torrent targets. Parse logs report plan reuse,
    hardlink work, client-import stage durations, and total duration.
    Notifications communicate the final outcome. They run in a server-owned task group. Shutdown waits for those tasks until its deadline, then cancels them.

## Failure Classes

- request decode/auth failure
- client connectivity or login failure
- client inventory or file-detail failure
- release mismatch
- torrent parse mismatch
- filesystem hardlink failure
- client import or verify failure

## Verification Notes

Verified against code on 2026-08-28. The lifecycle is implementation-backed, not aspirational.

## Discovery and Concurrent Imports

Prowlarr discovery supplies candidate titles and torrent bytes to the same exact
planning and import path. One process serializes imports per configured client
endpoint. Import checks the candidate gate again before it uses a cached plan.
Client mutation attempts invalidate cached inventories and plans for all aliases
of that endpoint, even when the adapter returns an error. A retry must establish
whether the earlier attempt already added the pack. An in-flight inventory scan
cannot restore a cache entry invalidated by an import.

See [Prowlarr discovery](../PROWLARR.md) for search and
schedule behavior.

## Operator Log Contract

Operators debug with these logs and notifications. Keep them stable, and update
[API.md](../API.md#notifications-and-logs) when they change.

- Match and import notifications use the action labels `Match` and `Import`.
- `/api/candidate` logs each release compatibility rejection as a structured
  event. The event reports the incompatible field, the original announced `want`
  value, the original client `got` value, and the client release. Fuzzy matching
  can normalize values for comparison but not for diagnostics.
- When `/api/candidate` passes, it logs each matching client release at `DEBUG`.
- `/api/match` logs the reusable, unmatched, and total torrent episode counts.
- Each unmatched torrent episode produces one structured log event. The event
  reports either a missing source episode, a duplicate torrent target, or the
  closest same-episode client source plus every incompatible safety field. Each
  incompatible field reports its torrent `want` and client `got` value.
- `/api/import` logs whether it reused or rebuilt the import plan. It also logs
  each successful or failed destination-resolution attempt at `INFO`, hardlink
  duration, each torrent-client import stage, total client-import duration, and
  total import duration. A missing hardlink source logs a static warning with a
  structured `source` path, followed by the bounded inventory and plan refresh
  result.
- Torrent-client stages use the stable names `config`, `add`, `find`, `recheck`,
  and `resume`. A client omits stages that it does not perform. Deluge reports a
  torrent that does not leave its paused state under `resume`. The client-import
  completion log reports `data_complete`: whether every torrent file was already
  on disk, which lets the client skip its hash check.
- A successful `/api/import` means the client accepted the pack. The client
  checks the present data and starts the torrent after the response.
- Logs never include configured credentials or request authentication tokens.
- Expected candidate and match gate rejections use informational events.
  Configuration, decoding, client, filesystem, and import failures use error
  events.
