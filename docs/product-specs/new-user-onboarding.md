# New User Onboarding

## User Goal

Get `seasonpackarr` running with autobrr and a supported torrent client so season packs reuse already-downloaded episodes.

## Happy Path

1. Install the binary, container, or service.
2. Start once to generate a default config.
3. Set host/port and `apiToken`.
4. Configure one or more torrent clients with the matching `type` and an `import` policy:
   - `qbittorrent` (default): username/password credentials, a qBittorrent 5.2.0+ `apiKey`, or a qui reverse-proxy host with client auth left blank; must set either `import.savePath` or `import.category`.
   - `transmission`: username/password credentials (no `apiKey`); the RPC protocol version is auto-negotiated; set `import.savePath` or leave it empty to use the session download directory.
   - `deluge-v1` or `deluge-v2`: matching native daemon RPC credentials (not Deluge Web); `deluge` is a V2 alias; port `58846` is used when unset; `import.savePath` is required; omit `apiKey` and qBittorrent-only fields; use at most one `import.tags` entry and enable Deluge's Label plugin if it must be applied.
5. Tune matching options such as smart mode and fuzzy matching.
6. Add two ordered autobrr external webhook checks: announce-only `/api/candidate`, then torrent-aware `/api/pack`.
   The reorder arrows appear only after multiple external checks exist. Save and reload, then confirm candidate is
   displayed above pack because the persisted display order is the execution order.
7. Add one Webhook action on `/api/parse` for hardlink creation and client import. Do not add a torrent-client action.
8. Run smoke tests with the CLI helper commands.

## First Success Criteria

- service starts cleanly
- health endpoints respond
- authenticated test payloads succeed
- expected hardlinks appear in the target season-pack folder
- the season pack appears in the torrent client and starts after verification
- logs explain what happened

## Common Failure Modes

- wrong or missing `import.savePath` (qBittorrent clients must set `import.savePath` or `import.category`)
- old `metadata`, `parseTorrentFile`, or `preImportPath` settings are still
  present (startup fails with a migration message)
- a missing `/api/candidate` check or a `/api/pack` payload without torrent bytes
- missing or wrong API token
- torrent client credentials/connectivity failure
- qui reverse-proxy URL entered without its `/proxy/...` path
- client import or verification failure
- a Deluge Web port or HTTP URL used instead of the native daemon RPC endpoint
- a pure BitTorrent v2 pack sent to the current seasonpackarr Deluge adapter
- a leftover autobrr torrent-client action adding the torrent a second time
- overly strict or overly loose fuzzy matching
