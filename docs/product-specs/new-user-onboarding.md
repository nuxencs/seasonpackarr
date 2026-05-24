# New User Onboarding

## User Goal

Get `seasonpackarr` running with autobrr and qBittorrent so season packs reuse already-downloaded episodes.

## Happy Path

1. Install the binary, container, or service.
2. Start once to generate a default config.
3. Set host/port and `apiToken`.
4. Configure one or more qBittorrent clients with a `qbit` import policy and either username/password credentials, a qBittorrent 5.2.0+ `apiKey`, or a qui reverse-proxy host with client auth left blank.
5. Tune matching options such as smart mode and fuzzy matching.
6. Add an autobrr external webhook filter for `/api/pack` and a webhook action for `/api/parse`.
7. Run a smoke test with the CLI helper commands.

## First Success Criteria

- service starts cleanly
- health endpoints respond
- authenticated test payloads succeed
- expected hardlinks appear in the target season-pack folder
- the season pack appears in qBittorrent and resumes after recheck if needed
- logs explain what happened

## Common Failure Modes

- wrong `qbit.savePath` or qBittorrent category save path
- missing or wrong API token
- qBittorrent credentials/connectivity failure
- qui reverse-proxy URL entered without its `/proxy/...` path
- unexpected qBittorrent default save path in category-only mode
- overly strict or overly loose fuzzy matching
