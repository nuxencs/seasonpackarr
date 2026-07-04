# New User Onboarding

## User Goal

Get `seasonpackarr` running with autobrr and a torrent client (qBittorrent or Transmission) so season packs reuse already-downloaded episodes.

## Happy Path

1. Install the binary, container, or service.
2. Start once to generate a default config.
3. Set host/port and `apiToken`.
4. Configure one or more torrent clients with the matching `type` and an `import` policy:
   - `qbittorrent` (default): username/password credentials, a qBittorrent 5.2.0+ `apiKey`, or a qui reverse-proxy host with client auth left blank; must set either `import.savePath` or `import.category`.
   - `transmission`: username/password credentials (no `apiKey`); the RPC protocol version is auto-negotiated; set `import.savePath` or leave it empty to use the session download directory.
5. Tune matching options such as smart mode and fuzzy matching.
6. Add an autobrr filter with an external webhook check on `/api/pack` (match gate) and a single Webhook action on `/api/parse` (hardlink + client import); no separate qBittorrent/Transmission action.
7. Run a smoke test with the CLI helper commands.

## First Success Criteria

- service starts cleanly
- health endpoints respond
- authenticated test payloads succeed
- expected hardlinks appear in the target season-pack folder
- the season pack appears in the torrent client and starts after verification
- logs explain what happened

## Common Failure Modes

- wrong or missing `import.savePath` (qBittorrent clients must set `import.savePath` or `import.category`)
- leftover `parseTorrentFile` or `preImportPath` settings from an older version (startup fails with a migration message)
- missing or wrong API token
- torrent client credentials/connectivity failure
- qui reverse-proxy URL entered without its `/proxy/...` path
- client import or verification failure
- a leftover autobrr qBittorrent/Transmission action adding the torrent a second time
- overly strict or overly loose fuzzy matching
