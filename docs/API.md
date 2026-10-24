# API

seasonpackarr has a small HTTP API. autobrr calls the candidate, match, and
import endpoints (see the [autobrr setup](AUTOBRR.md)). The
[command-line tools](CLI.md) call the same endpoints. Search runs
[Prowlarr discovery](PROWLARR.md) on demand.

## Endpoints

| Method | Path | Authentication | Body | Purpose |
| --- | --- | --- | --- | --- |
| `POST` | `/api/candidate` | API token | `name`, `clientname` | Candidate check of a release name. Downloads no torrent and changes nothing. |
| `POST` | `/api/match` | API token | `name`, `torrent`, `clientname` | Match: builds an import plan from the torrent. Changes nothing. |
| `POST` | `/api/import` | API token | `name`, `torrent`, `clientname` | Import: creates hardlinks and adds the season pack to the client. |
| `POST` | `/api/search` | API token | See [Search](#search) | Runs a targeted Prowlarr search. |
| `GET` | `/api/healthz/liveness` | None | None | Returns `200` with the text `OK` while the process runs. |
| `GET` | `/api/healthz/readiness` | None | None | Returns `200` with the text `OK`. It does not check the clients or Prowlarr. |

## Authentication

When `apiToken` is set in the config, every endpoint except the health
endpoints needs the token. Send it in one of these ways:

- the `X-API-Token` request header
- the `apikey` query parameter, for example `/api/import?apikey=<token>`

The header has priority when both are present. A missing or wrong token gets
`401` with `{"error":"unauthorized"}`. When `apiToken` is empty, the API does
not check tokens. Generate a token with `seasonpackarr gen-token`.

## Candidate, match, and import requests

The body is one JSON object:

```json
{
  "name": "Show.S01.1080p.WEB-DL.DDP5.1.H.264-RlsGrp",
  "torrent": "<torrent file bytes>",
  "clientname": "default"
}
```

- `name`: the release name from the announce. It is required.
- `torrent`: the bytes of the `.torrent` file, as a base64 string or as the
  byte list that autobrr writes for `{{ .TorrentDataRawBytes | js }}`. Match
  and import require it. The candidate check ignores it.
- `clientname`: the name of a client under `clients` in the config. When it is
  empty, seasonpackarr uses the client named `default`.

### What each endpoint does

- **Candidate check** (`/api/candidate`) compares the release name with the
  episode torrents in the client. It reads torrent summaries only. It never
  requests torrent bytes or per-torrent file details, so a rejection here saves
  the torrent download.
- **Match** (`/api/match`) reads the torrent file list. It counts MKV and MP4
  files that parse as episodes and ignores samples and extra videos. It finds a
  reusable episode for each distinct episode target, applies
  [smart mode](CONFIGURATION.md#smart-mode), and keeps the accepted import plan
  in memory for a short time. It does not change files or the client.
- **Import** (`/api/import`) uses the import plan from the match, or builds it
  again when the plan is no longer in memory, for example after a restart. It
  resolves the destination folder, creates the hardlinks, and adds the season
  pack through the client's import policy. If a planned source file disappears,
  it refreshes the client inventory and the plan once and tries again. If the
  coverage of the created hardlinks falls below the smart mode threshold, it
  does not add the pack. Hardlinks that were created stay in place, so a retry
  is safe.

A successful import means the client accepted the pack. The client checks the
data and starts the torrent after the response. seasonpackarr does not wait for
that check.

A normal candidate, match, import sequence reads the client inventory once. The
three requests share a short-lived copy of it.

### Imports to one client

Imports to the same client run one at a time. This includes imports from
autobrr, RSS monitoring, and search. Before an import uses a stored import
plan, it runs the candidate check again. So a request that matched before can
get `210` (release already in client) when another import added the pack first.
After each import attempt, seasonpackarr discards its stored inventory and
plans for that client, also when the attempt failed. A retry reads the client
state again before it imports.

## Responses

A successful request gets status `250` with the text `successful match`. Match
and import both use `250`.

A rejection or failure gets the status code from the table below, with a JSON
body:

```json
{"statusCode": 203, "error": "release group did not match"}
```

A body that is not valid JSON gets `470`.

## Status codes

The notification level is the `notificationLevel` value that sends a Discord
notification for the code. Codes without a level send no notification.

| Code | Meaning | Notification level |
| --- | --- | --- |
| `200` | No matching releases in the client | `INFO` |
| `201` | Resolution did not match | `INFO` |
| `202` | Source did not match | `INFO` |
| `203` | Release group did not match | `INFO` |
| `204` | Cut did not match | `INFO` |
| `205` | Edition did not match | `INFO` |
| `206` | Repack status did not match | `INFO` |
| `207` | HDR metadata did not match | `INFO` |
| `208` | Streaming service did not match | `INFO` |
| `210` | Release already in the client | `INFO` |
| `211` | Release is not a season pack | `INFO` |
| `212` | Size did not match | None |
| `213` | Season did not match | None |
| `214` | Episode did not match | None |
| `215` | Container did not match | `INFO` |
| `230` | Coverage is below the smart mode threshold | `INFO` |
| `250` | Successful match or import | `MATCH` |
| `440` | Could not create hardlinks | `ERROR` |
| `445` | Could not match episodes to files in the pack | `ERROR` |
| `459` | Invalid import config, for example a qBittorrent category that does not exist | `ERROR` |
| `460` | Could not resume the torrent in the client | `ERROR` |
| `461` | Could not recheck the torrent in the client | `ERROR` |
| `462` | Could not find the added torrent in the client | `ERROR` |
| `463` | Could not add the torrent to the client | `ERROR` |
| `464` | Could not get episodes | `ERROR` |
| `465` | Could not parse the torrent info | `ERROR` |
| `466` | Could not decode the torrent bytes | `ERROR` |
| `467` | Could not get the torrent bytes | `ERROR` |
| `468` | Could not get torrents from the client | `ERROR` |
| `469` | Could not get the announce name | `ERROR` |
| `470` | Could not decode the request | `ERROR` |
| `471` | Could not get the client | `ERROR` |
| `472` | Client not found in the config | `ERROR` |

The current endpoints do not return codes `212` to `215`, and the logs do not
contain them. For per-episode diagnostics, read the
`torrent episode is not reusable` log lines. Their `reason` field
(`source_episode_not_found`, `compatibility_mismatch`, or
`duplicate_torrent_target`) and `mismatches[].field` (`size`, `container`,
`resolution`, or `release_group`) explain why an episode was not reused.

## Notifications and logs

- The candidate check sends no notifications. Match sends notifications with
  the action label `Match`, and import with the label `Import`. An accepted
  pack therefore sends two `MATCH` notifications: one from match and one from
  import.
- The candidate check and match log expected rejections, such as a release
  group mismatch, at `INFO`. Configuration, decoding, client, filesystem, and
  import failures are logged at `ERROR`. The import logs every failure at
  `ERROR`, also an expected rejection
  ([#194](https://github.com/nuxencs/seasonpackarr/issues/194)).
- When a match does not use a torrent episode, the log shows why: a missing
  source episode, a duplicate target, or the closest client file and each field
  that did not match.
- Logs never include configured credentials or request tokens.

## Search

`POST /api/search` runs a targeted Prowlarr search. It does not run RSS
monitoring. The body is one JSON object. Unknown fields are rejected.

```json
{"clientname": "default", "dryRun": true, "verify": false}
```

- `clientname`: one client to scan. Omit it to scan all configured clients.
- `dryRun`: when `true`, report results without importing. Default `false`.
- `verify`: when `true`, also download torrent metadata and check exact reuse.
  Requires `dryRun: true`. Default `false`.

| Status | Meaning |
| --- | --- |
| `200` | The run finished. The report can still contain failures. |
| `400` | Invalid request, unknown client, or incomplete search configuration. |
| `401` | Missing or wrong API token, when authentication is configured. |
| `409` | Another RSS or search run is active. |
| `500` | The database could not be read before the run. Check the service logs. |

The `200` report contains these fields:

- `dryRun` and `verify`: the request values.
- `scannedTorrents`, `episodeTorrents`, `coveredEpisodeTorrents`, and `groups`:
  inventory counts. See [Search output](PROWLARR.md#search-output).
- `requests`: targeted feed requests. Indexer discovery and torrent downloads
  are not counted.
- `torrentDownloads`: completed metadata downloads. A failed download appears
  as a failed outcome. `torrentCacheHits`: metadata reads from the cache that
  saved a download.
- `outcomes[]`: one entry per result, with `clientname`, `title`, `indexerId`,
  `status`, `reason`, `reusableEpisodes`, and `totalEpisodes`. `status` is
  `candidate`, `would_import`, `imported`, `rejected`, or `failed`. The episode
  counts are `null` until an import plan sets the number of targets. A plan
  with no reusable files reports `0`.
- `failures[]`: discovery, client inventory, query, pagination, rate limit,
  database, and cancellation errors, with `clientname`, `query`, `indexerId`,
  and `reason` where they apply.

A `200` status alone does not prove that all work succeeded. Check `failures`.

## Changes from v0.16.0

- `/api/pack` and `/api/parse` are removed. They return `404`. There are no
  aliases or redirects.
- `/api/candidate` is new. It takes `name` and `clientname`, like the old
  `/api/pack`.
- `/api/match` replaces the check part of `/api/pack`. It requires `torrent`
  and never creates hardlinks. In v0.16.0, `/api/pack` could create hardlinks
  when `parseTorrentFile` was `false`.
- `/api/import` replaces `/api/parse`. It now also adds the season pack to the
  client, so autobrr needs no torrent client action.

The [v1 upgrade guide](UPGRADE-v1.md) shows how to update autobrr and scripts.
