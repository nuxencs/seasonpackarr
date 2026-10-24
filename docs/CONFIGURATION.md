# Configuration

seasonpackarr reads `config.yaml`. The first start writes a commented file with every setting, so most changes are edits
to that file. The [JSON schema](../schemas/config-schema.json) gives editor completion and checks values.

This page explains the settings that need a decision. For the Prowlarr settings under `search`, see
[Prowlarr discovery](PROWLARR.md).

## Config file location

`seasonpackarr start --config <dir>` uses `<dir>/config.yaml` and creates it when it is missing. Docker and the systemd
service always pass `--config`. Without `--config`, seasonpackarr uses the first file it finds:

1. `./config.yaml`
2. `~/.config/seasonpackarr/config.yaml`
3. `~/.seasonpackarr/config.yaml`

## Server settings

| Setting | Default | Meaning |
| --- | --- | --- |
| `host` | `"0.0.0.0"` | Address the API listens on. The command line uses it too, with a wildcard address changed to loopback. |
| `port` | `42069` | Port the API listens on. |
| `apiToken` | empty | Token for API requests. Generate one with `seasonpackarr gen-token`. When empty, the API has no authentication. |

Set `apiToken`. autobrr and the command line send it in the `X-API-Token` header or the `apikey` query parameter.

## Torrent clients

Each entry under `clients` connects seasonpackarr to one torrent client. The entry name, for example `default`, is the
client name that autobrr and the command line use. Each name must be unique.

| Setting | Meaning |
| --- | --- |
| `type` | `"qbittorrent"` (default), `"transmission"`, `"deluge-v1"`, or `"deluge-v2"`. `"deluge"` is an alias for `"deluge-v2"`. |
| `host` | Hostname or IP address of the client. |
| `port` | Port of the client API. Set it explicitly. Only Deluge has a fallback (`58846`). |
| `username`, `password` | Client credentials. |
| `apiKey` | qBittorrent only. See [qBittorrent](#qbittorrent). |
| `import` | How seasonpackarr adds season packs to this client. See [Import policy](#import-policy). |

### qBittorrent

Supported versions: 4.3.9 to 5.2.4. The integration tests check 4.3.9, 4.5.5, 4.6.7, 5.0.5, 5.1.4, and 5.2.4.

- Authenticate with `username` and `password`, or with `apiKey` on qBittorrent 5.2.0 or newer. When `apiKey` is set,
  seasonpackarr uses it and ignores `username` and `password`.
- With [qui's reverse proxy](https://getqui.com/docs/features/reverse-proxy/), set `host` to the full proxy URL, for
  example `http://localhost:7476/proxy/abc123...`, and leave `username`, `password`, and `apiKey` empty. qui handles
  the authentication. The URL must include its `/proxy/...` path.
- qBittorrent listens on port `8080` by default.

### Transmission

Supported versions: 4.0.6 to 4.1.3. The integration tests check 4.0.6 and 4.1.3.

- Set `type: "transmission"`.
- Authenticate with `username` and `password` for the Transmission RPC interface. Transmission has no `apiKey`.
- Transmission listens on port `9091` by default. Set `port: 9091`, because seasonpackarr does not assume a port.

### Deluge

Supported versions: 1.3.15, and 2.0.3 to 2.2.0. The integration tests check 1.3.15, 2.0.3, and 2.2.0.

seasonpackarr connects to the Deluge daemon RPC interface through
[`autobrr/go-deluge`](https://github.com/autobrr/go-deluge), not to Deluge Web.

- Set `type: "deluge-v1"` for Deluge 1.3 or `type: "deluge-v2"` for Deluge 2. The two versions use different wire
  formats, so the type must match the daemon.
- Use an account from the Deluge `auth` file as `username` and `password`.
- Set `host` to a hostname or IP address, not an HTTP URL. Leave `port` unset to use the daemon port `58846`. Do not use
  the Deluge Web port.
- `import.savePath` is required.
- `import.tags` accepts zero or one entry, which becomes the torrent's label. The label can contain letters, digits,
  underscores, and hyphens. Enable the Deluge Label plugin to apply it. seasonpackarr creates a missing label.
- `apiKey` and the qBittorrent-only import fields are not allowed.
- The torrent must be a v1 or hybrid torrent. The adapter identifies torrents by their v1 info hash, so it rejects pure
  BitTorrent v2 torrents.

The [Deluge primary-source audit](research/deluge-primary-source-audit.md) records the autobrr, go-deluge, and Deluge
revisions behind these rules.

## Import policy

The `import` block of each client controls where a season pack goes and how seasonpackarr adds it to the client.

### Fields for every client

- **`savePath`**: the final destination. seasonpackarr hardlinks the reusable episodes into the season pack folder in
  this directory and adds the torrent with this save path. When set, the directory must already exist, or startup fails.
  - qBittorrent: optional. Without it, the destination follows the category and the qBittorrent automatic management
    and category path preferences.
  - Transmission: optional. Without it, seasonpackarr uses the session download directory.
  - Deluge: required.
- **`tags`**: qBittorrent tags or Transmission labels for imported torrents. Deluge accepts at most one entry, as
  described in [Deluge](#deluge). There is no default: if you remove `tags`, imported torrents get no tag. The generated
  config sets `["seasonpackarr"]`.

### qBittorrent-only fields

Startup fails when a Transmission or Deluge client sets one of these fields.

- **`category`**: the category for the added torrent. When `savePath` is empty, the category also sets the destination,
  and it must already exist in qBittorrent. A missing category makes the import fail with status `459`. When no save path
  and no download path are set, seasonpackarr sends only the category and lets qBittorrent choose the path. It reads
  the same qBittorrent preferences before it creates hardlinks, so both programs use the same destination.
- **`downloadPath`**: a temporary path for incomplete downloads. It is never the final destination. When set, the
  directory must already exist, or startup fails.
- **`contentLayout`**: `"subfolder"`, `"nosubfolder"`, or `"original"`. Leave it empty to use the qBittorrent default.

A qBittorrent client must set `savePath` or `category`.

### What happens during an import

The season pack folder gets its name from the torrent, not from the announce. An announce can differ from the folder
name, for example `Show.S01.1080p.WEB-DL.DDPA5.1.H.264-RlsGrp` against the folder
`Show.S01.1080p.WEB-DL.DDP5.1.H.264-RlsGrp`. A folder with the announce name would not match the torrent, and the client
would download every file again.

seasonpackarr does not wait for the client's data check, so a large pack cannot go past the 120-second autobrr action
timeout.

- **qBittorrent**: when every file of the pack is already on disk, qBittorrent adds it without a hash check. Otherwise
  it checks the present data and downloads only the missing pieces.
- **Transmission**: when every file is already on disk, Transmission checks the first piece and accepts the pack as
  complete. Otherwise it checks the present data and downloads only the missing pieces.
- **Deluge**: it always checks the present data, then downloads the missing pieces.

seasonpackarr reuses only video files. A pack that also contains files such as `.nfo` or subtitles is partial, so the
client checks it. An imported pack is never left stopped.

### Use multiple Sonarr instances with one qBittorrent instance

When several Sonarr instances share one qBittorrent instance, add that qBittorrent instance once for each Sonarr
instance. Give each entry a unique name and the category of its Sonarr instance:

```yaml
clients:
  sonarr-hd:
    type: "qbittorrent"
    host: "127.0.0.1"
    port: 8080
    username: "admin"
    password: "your-password"
    import:
      category: "tv-hd"

  sonarr-uhd:
    type: "qbittorrent"
    host: "127.0.0.1"
    port: 8080
    username: "admin"
    password: "your-password"
    import:
      category: "tv-uhd"
```

In qBittorrent, set each category to save to the folder of its Sonarr instance. With autobrr, create one filter for
each entry, and use the entry name as `clientname` in that filter. See [Multiple clients](AUTOBRR.md#multiple-clients).

Both entries see the same qBittorrent torrent list. qBittorrent stores a torrent only once, so it cannot add the same
torrent to both categories.

## Smart mode

Smart mode rejects a season pack when too few of its episodes are already on disk.

| Setting | Default | Meaning |
| --- | --- | --- |
| `smartMode` | `false` | Turns smart mode on. |
| `smartModeThreshold` | `0.75` | The minimum coverage that a pack needs. |

Coverage is the share of distinct episode files in the torrent that seasonpackarr can reuse from the client. MKV and MP4
episodes count. Samples, extra videos, and files with a different container do not count. seasonpackarr reads the
episode count from the torrent and uses no external episode database.

Example: the torrent has 12 episode files and the client has 8 reusable episodes. The coverage is `8/12 = 0.67`, so a
threshold of `0.75` rejects the pack. Coverage cannot go above 100 percent, even when the client has more episodes than
the pack.

### When hardlinks fail during the import

When smart mode is on, the import checks the coverage again after it creates the hardlinks. If too few links succeed for
the threshold, the import stops before it adds the torrent, and the response has status `230` (below threshold).
Without smart mode, the import stops only when no link succeeds (status `440`).

Links that the import already created stay in place. seasonpackarr does not delete them, because they could belong to
another application.

Partial-season packs, specials, and torrents with an unusual file layout can give a different result than in v0.16.0.
Check the first few matches, and change the threshold only if the result does not fit your preference.

## Fuzzy matching

By default, a season pack must match the episodes in the client exactly. The `fuzzyMatching` settings relax single
comparisons. All are `false` by default.

| Setting | Effect when `true` |
| --- | --- |
| `skipRepackCompare` | Ignores the repack status. The pack `Show.S01.1080p.WEB-DL.DDPA5.1.H.264-RlsGrp` then matches the episode `Show.S01E01.1080p.WEB-DL.REPACK.DDPA5.1.H.264-RlsGrp`. |
| `simplifyHdrCompare` | Treats `HDR10`, `HDR10+`, and `HDR+` as `HDR`. The pack `Show.S01.2160p.WEB-DL.DDPA5.1.DV.HDR10+.H.265-RlsGrp` then matches the episode `Show.S01E01.2160p.WEB-DL.DDPA5.1.DV.HDR.H.265-RlsGrp`. |
| `simplifyWebCompare` | Treats `WEB-DL` and `WEB` as the same source. |
| `skipYearCompare` | Lets a release without a year match a release with a year. |

Each relaxed comparison accepts more packs. A wrong match costs more than a pack that downloads in full, so turn on only
the comparisons that your trackers need.

## Recommended settings

These settings work well for most setups. Adjust them to your trackers.

```yaml
smartMode: true
smartModeThreshold: 0.75
fuzzyMatching:
  skipRepackCompare: true
  simplifyHdrCompare: false
```

They reject most unwanted season packs and still match renamed packs and episodes.

## Notifications

seasonpackarr can send notifications to a Discord webhook.

| Setting | Default | Meaning |
| --- | --- | --- |
| `notifications.discord` | empty | Discord webhook URL. Empty turns notifications off. |
| `notifications.notificationLevel` | `["MATCH", "ERROR"]` | Which events to send. |

- `MATCH`: an accepted season pack. The match and the import each send one, so an accepted pack sends two.
- `INFO`: a rejected season pack from a match or an import. The candidate check sends no notifications.
- `ERROR`: a failed request, for example a client connection error.

## Logging

| Setting | Default | Meaning |
| --- | --- | --- |
| `logLevel` | `"DEBUG"` | `"ERROR"`, `"WARN"`, `"INFO"`, `"DEBUG"`, or `"TRACE"`. |
| `logPath` | empty | Log file. When empty, seasonpackarr logs to stdout. Use forward slashes and include the file name, for example `logs/seasonpackarr.log`. |
| `logMaxSize` | `50` | Maximum log file size in megabytes. |
| `logMaxBackups` | `3` | Number of old log files to keep. |

## Environment variables

Each setting has an environment variable with the `SEASONPACKARR__` prefix. Environment variables override
`config.yaml`. The repository's [`docker-compose.yml`](../docker-compose.yml) lists the common ones.

- Top-level settings: `SEASONPACKARR__HOST`, `SEASONPACKARR__PORT`, `SEASONPACKARR__API_TOKEN`,
  `SEASONPACKARR__LOG_LEVEL`, `SEASONPACKARR__LOG_PATH`, `SEASONPACKARR__LOG_MAX_SIZE`, `SEASONPACKARR__LOG_MAX_BACKUPS`,
  `SEASONPACKARR__SMART_MODE`, `SEASONPACKARR__SMART_MODE_THRESHOLD`.
- Fuzzy matching: `SEASONPACKARR__FUZZY_MATCHING_SKIP_REPACK_COMPARE`,
  `SEASONPACKARR__FUZZY_MATCHING_SIMPLIFY_HDR_COMPARE`, `SEASONPACKARR__FUZZY_MATCHING_SIMPLIFY_WEB_COMPARE`.
  `skipYearCompare` has no environment variable.
- Notifications: `SEASONPACKARR__NOTIFICATIONS_DISCORD`, and `SEASONPACKARR__NOTIFICATIONS_NOTIFICATION_LEVEL` as a
  comma-separated list, for example `MATCH,ERROR`.
- Clients: `SEASONPACKARR__CLIENTS_<NAME>_<SETTING>`. `<NAME>` is the client name in upper case. The client name
  becomes lower case. `<SETTING>` is `TYPE`, `HOST`, `PORT`, `USERNAME`, `PASSWORD`, `APIKEY`, `IMPORT_SAVE_PATH`,
  `IMPORT_CATEGORY`, `IMPORT_DOWNLOAD_PATH`, `IMPORT_CONTENT_LAYOUT`, or `IMPORT_TAGS` (comma-separated). In these
  variables, a client name cannot contain an underscore.
- Prowlarr: see [Prowlarr discovery](PROWLARR.md).

seasonpackarr ignores an environment variable with an empty value.

To run without `config.yaml`, set `SEASONPACKARR__DISABLE_CONFIG_FILE=true` and configure everything with environment
variables. The database then moves, see [Database location](INSTALL.md#database-location).

## Config reload

seasonpackarr watches the active `config.yaml` while it runs. For each change, it loads the complete file, applies the
environment variables again, and validates the result. Only a valid result becomes active. When the file is not valid,
for example during a partial write, seasonpackarr logs the error and keeps the last valid config.

These settings apply without a restart:

- torrent client connections and import policies
- API token
- smart mode and fuzzy matching
- Discord webhook and notification levels
- log level
- Prowlarr connection, request spacing, and RSS interval

These settings need a restart:

- `host` and `port`
- `logPath`, `logMaxSize`, and `logMaxBackups`
- `SEASONPACKARR__DISABLE_CONFIG_FILE`

When you remove an optional setting, it returns to its default. A running search or RSS run keeps the config it started
with. A changed RSS interval sets the time of the next automatic run.
