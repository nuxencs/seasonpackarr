# Configuration

seasonpackarr reads `config.yaml`. The first start writes a commented file with every setting, so most changes are edits
to that file. The [JSON schema](../schemas/config-schema.json) gives editor completion and checks values.

This page lists every setting and explains the settings that need a decision. For how Prowlarr discovery works, see
[Prowlarr discovery](PROWLARR.md).

## Config file location

`seasonpackarr start --config <dir>` uses `<dir>/config.yaml` and creates it when it is missing. Docker and the systemd
service always pass `--config`. Without `--config`, seasonpackarr uses the first file it finds:

1. `./config.yaml`
2. `~/.config/seasonpackarr/config.yaml`
3. `~/.seasonpackarr/config.yaml`

In Docker, the working directory is `/config`, so a command that you run with `docker exec` finds `/config/config.yaml`.

## Unknown keys

seasonpackarr checks each key in `config.yaml`. A key that seasonpackarr does not know stops startup, for example a
typo such as `logLevl` or a key from a different version. The error names each unknown key with its full path:

```text
validating config file /config/config.yaml: unknown config keys: clients.default.hots, logLevl; remove or rename them (see the configuration guide)
```

Remove or rename each key that the error names, and start again. The [JSON schema](../schemas/config-schema.json) lists
all keys, and an editor with schema support shows unknown keys before you start.

seasonpackarr reads key names without case, so `logLevel` and `LogLevel` are the same key. The schema is case-sensitive
and marks `LogLevel` as unknown, so use the spelling from the schema. If the file has one key twice with a different
case, seasonpackarr uses one of them and reports the other as unknown.

The settings that v0.16.0 removed (`parseTorrentFile`, `metadata`, and `clients.<name>.preImportPath`) give a more
specific error with the migration step. See [Remove old settings](UPGRADE-v1.md#remove-old-settings).

The command-line commands read only the connection settings from `config.yaml`. They ignore unknown keys, so they also
work with a config from a different version.

## Server settings

| Setting | Default | Meaning |
| --- | --- | --- |
| `host` | `"0.0.0.0"` | Address the API listens on. The command line uses it too, with a wildcard address changed to loopback. |
| `port` | `42069` | Port the API listens on, from `1` to `65535`. |
| `apiToken` | empty | Token for API requests. Generate one with `seasonpackarr gen-token`. When empty, the API has no authentication. |

Set `apiToken`. autobrr and the command line send it in the `X-API-Token` header or the `apikey` query parameter.

## Torrent clients

Each entry under `clients` connects seasonpackarr to one torrent client. The entry name, for example `default`, is the
client name that autobrr and the command line use. Each name must be unique. In the tables, `<name>` is the client
name.

| Setting | Meaning |
| --- | --- |
| `clients.<name>.type` | `"qbittorrent"` (default), `"transmission"`, `"deluge-v1"`, or `"deluge-v2"`. `"deluge"` is an alias for `"deluge-v2"`. |
| `clients.<name>.host` | Hostname or IP address of the client. It is required. qBittorrent and Transmission also accept a URL, such as `http://qbit:8080`. A Deluge host must not have a URL scheme, user info, or a path. See [Bad values](#bad-values). |
| `clients.<name>.port` | Port of the client API, at most `65535`. Set it explicitly. Only Deluge has a fallback (`58846`). |
| `clients.<name>.username`, `clients.<name>.password` | Client credentials. |
| `clients.<name>.apiKey` | qBittorrent only. See [qBittorrent](#qbittorrent). |
| `clients.<name>.import` | How seasonpackarr adds season packs to this client. See [Import policy](#import-policy). |
| `clients.<name>.inventory` | qBittorrent only. Limits the torrents that seasonpackarr reads from this client. See [Inventory scope](#inventory-scope). |
| `clients.<name>.sourceFolders` | Absolute paths of folders whose episode files are part of the inventory of this client. Each client type. See [Source folders](#source-folders). |

### qBittorrent

Supported versions: 4.3.9 to 5.2.4. The integration tests check 4.3.9, 4.5.5, 4.6.7, 5.0.5, 5.1.4, and 5.2.4.

- Authenticate with `username` and `password`, or with `apiKey` on qBittorrent 5.2.0 or newer. When `apiKey` is set,
  seasonpackarr uses it and ignores `username` and `password`.
- With [qui's reverse proxy](https://getqui.com/docs/features/reverse-proxy/), set `host` to the full proxy URL, for
  example `http://localhost:7476/proxy/abc123...`, and leave `username`, `password`, and `apiKey` empty. qui handles
  the authentication. The URL must include its `/proxy/...` path. The path is a key, so errors and logs show it as
  `[redacted]`.
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

| Setting | Meaning |
| --- | --- |
| `clients.<name>.import.savePath` | The final destination. seasonpackarr hardlinks the reusable episodes into the pack folder in this directory and adds the torrent with this save path. The path must be absolute. A relative path fails the import with status `440`, and seasonpackarr makes no hardlink. When set, the directory must already exist, or startup fails. Optional for qBittorrent: without it, the destination follows the category and the qBittorrent automatic management and category path preferences. Optional for Transmission: without it, seasonpackarr uses the session download directory. Required for Deluge. |
| `clients.<name>.import.tags` | qBittorrent tags or Transmission labels for imported torrents. Deluge accepts at most one entry, as described in [Deluge](#deluge). There is no default: if you remove `tags`, imported torrents get no tag. The generated config sets `["seasonpackarr"]`. |

### qBittorrent-only fields

Startup fails when a Transmission or Deluge client sets one of these fields.

| Setting | Meaning |
| --- | --- |
| `clients.<name>.import.category` | The category for the added torrent. When `savePath` is empty, the category also sets the destination, and it must already exist in qBittorrent. A missing category makes the import fail with status `459`. When no save path and no download path are set, seasonpackarr sends only the category and lets qBittorrent choose the path. It reads the same qBittorrent preferences before it creates hardlinks, so both programs use the same destination. |
| `clients.<name>.import.downloadPath` | A temporary path for incomplete downloads. It is never the final destination. When set, the directory must already exist, or startup fails. |
| `clients.<name>.import.contentLayout` | `"subfolder"`, `"nosubfolder"`, or `"original"`, in any letter case. Leave it empty to use the qBittorrent default. |

A qBittorrent client must set `savePath` or `category`.

### What happens during an import

The pack folder gets its name from the torrent, not from the announce. An announce can differ from the folder
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

## Inventory scope

The inventory is the list of torrents that seasonpackarr reads from a client. The candidate check, the match, and
Prowlarr discovery use it. Without a scope, seasonpackarr reads every torrent in the client. On a client with many
torrents that seasonpackarr never reuses, such as movies, this read is a large part of each candidate check. A scope
limits the inventory to the torrents that seasonpackarr can reuse.

```yaml
clients:
  default:
    import:
      category: "tv-hd"
    inventory:
      includeCategories: [ "tv-hd", "tv-hd-completed", "tv-hd-completed.cross-seed" ]
      includeTags: []
      excludeCategories: []
      excludeTags: [ "noreuse" ]
```

| Setting | Meaning |
| --- | --- |
| `clients.<name>.inventory.includeCategories`, `clients.<name>.inventory.includeTags` | Read only the torrents with one of these categories or one of these tags. When both lists are empty, seasonpackarr reads every torrent. |
| `clients.<name>.inventory.excludeCategories`, `clients.<name>.inventory.excludeTags` | Drop the torrents with one of these categories or one of these tags. An exclude always wins over an include. |

When all four lists are empty, or the `inventory` block is missing, the scope is every torrent. Names must match
exactly.

The scope applies to the candidate check, the match, the checks of the import, and Prowlarr RSS and search. The import
itself ignores the scope: seasonpackarr adds the season pack with the [import policy](#import-policy), and it finds the
added torrent even when its category is outside the scope.

### Includes or excludes

qBittorrent can filter its torrent list by category and by tag, but it has no filter that excludes a value. So the two
kinds of lists have different costs:

- **Includes make the read smaller.** seasonpackarr sends one request for each include value, up to four at the same
  time, and merges the results. A torrent that is in more than one result counts once.
- **Excludes do not make the read smaller.** seasonpackarr removes the excluded torrents after the read. With excludes
  only, it reads every torrent, as it does without a scope.

Use includes for speed, and excludes for convenience, for example to remove one movie category without a list of all
TV categories. You can use both: the excludes then remove torrents from the include results.

A benchmark with a client like the one in [issue #326](https://github.com/nuxencs/seasonpackarr/issues/326) (5341
torrents, about 11 MB of JSON for the full list) gave these read times on a local test server. A remote client adds
its own delay to each request, so real times are longer.

| Scope | Torrents | Data | Time |
| --- | --- | --- | --- |
| None | 5341 | 11.0 MB | 28 ms |
| `excludeCategories: [movies]` | 3691 | 11.0 MB | 28 ms |
| The same torrents through four `includeCategories` | 3691 | 7.7 MB | 13 ms |
| Two `includeCategories` | 1204 | 2.5 MB | 5 ms |

qBittorrent 4.3 cannot filter by tag. When the scope has `includeTags` and the client is qBittorrent 4.3, seasonpackarr
also reads every torrent and finds the tags itself. The result is the same, but the read is not smaller.

### Effects of a scope

Before you set a scope, make sure that you accept these effects:

- **Fewer reusable episodes:** seasonpackarr does not reuse episodes of torrents outside the scope. More data downloads.
  A scope cannot cause a false match, because it only removes torrents from the inventory.
- **Packs outside the scope are not found:** the "release already in client" check (status `210`) and the Prowlarr
  coverage see only the scope. When a season pack is in a category outside the scope, seasonpackarr makes hardlinks
  for it again. The client then rejects the add (same info hash) or imports a duplicate (different info hash). Keep
  every category and tag where season packs end up in the scope. Excludes are safer here: a new category is in the
  scope until you exclude it.
- **The import category alone is not always enough:** Sonarr can move finished torrents to another category, for
  example `tv-hd-completed`. Then the import category holds only new packs, and the completed category holds the rest.
  Include both. A new pack stays in the import category until Sonarr moves it. If the scope drops a torrent with the
  import category and import tags, a second announce of the same pack does not find it. seasonpackarr logs a warning
  at startup and after a config reload when the scope drops such a torrent. The warning compares exact names, so it
  also warns for an import category such as `tv/new` with `includeCategories: [tv]`. That scope is correct on
  qBittorrent 5.2 and newer, and on older versions when subcategories are on.
- **Subcategories:** qBittorrent adds subcategories to an include category only for names with `/`. qBittorrent 5.2
  and newer always do this. Older versions do it only when subcategories are on in qBittorrent. A name such as `tv-hd-completed.cross-seed` is a separate category, so list it
  explicitly. Excludes match the exact name only: `excludeCategories: [movies]` does not exclude `movies/4k`.
- **qBittorrent and qui only:** startup fails when a Transmission or Deluge client sets a scope. An entry that is
  empty or starts or ends with a space also stops startup, because qBittorrent compares exact names.


## Source folders

A source folder is a folder whose episode files are part of the inventory of a client, although no torrent of the
client holds them. Examples are a library that Sonarr fills from Usenet downloads, the files of a torrent that you
removed from the client, and files that you copied in by hand. seasonpackarr reads the release name of each file from
the file name only, whichever tool put the file there.

```yaml
clients:
  default:
    sourceFolders: [ "/data/media/tv", "/data/media/anime" ]
```

| Setting | Default | Meaning |
| --- | --- | --- |
| `clients.<name>.sourceFolders` | `[]` | Absolute paths of the source folders of this client. Each client type supports them. The inventory scope does not apply to them. |
| `sourceFolderScanInterval` | `"15m"` | Time between two scans of the source folders of all clients, at least `"1m"`. See [Scan interval](#scan-interval). |

- Each path must be absolute. A relative path or an empty entry stops the load, also at a
  [config reload](#config-reload). seasonpackarr cleans each path (`/data/tv/` becomes `/data/tv`) and removes a path
  that is in the list two times.
- A folder that does not exist or that seasonpackarr cannot read does not stop the start, because a mount can come up
  late. seasonpackarr logs a warning with the folder and the client.
- The environment variable is `SEASONPACKARR__CLIENTS_<NAME>_SOURCE_FOLDERS`, with commas between the paths. A path
  that contains a comma needs `config.yaml`.
- To give two clients the same folders, use a YAML anchor on the list:

  ```yaml
  clients:
    hd:
      sourceFolders: &folders [ "/data/media/tv" ]
    uhd:
      sourceFolders: *folders
  ```

  Two clients with the same path share one scan of the folder.

seasonpackarr scans the source folders at startup, and then again each `sourceFolderScanInterval`. A request never
waits for a scan: before the first scan of a folder is complete, that folder supplies no file. After each scan, the log
has one line with the folder, the number of episode files, and the duration. The line `client inventory ready` of a
client with source folders also has the number of episode files from its source folders (`source_folder_files`).

- One scan runs at a time, and it reads one folder at a time. A scan that must start while another scan runs starts
  when that scan ends.
- A complete scan of a folder replaces the last result of that folder in one step.
- When seasonpackarr cannot read the folder itself, for example because a mount is gone for a short time, the scan
  logs a warning and keeps the last result. A folder that was missing at startup counts after the first scan that can
  read it, with no restart.
- A scan parses only the file names that the last scan of the folder did not have. This includes the names that the
  last scan did not keep.
- A [config reload](#config-reload) applies a changed folder list and a changed interval with no restart. A new folder
  gets a scan at once. The result of a folder that no client names is dropped.

The scan uses these rules:

- It reads the whole tree below the folder, with no depth limit.
- It skips each file and each folder whose name starts with a dot, for example a trash folder or a metadata folder.
- It does not follow a symlink below the folder, and a symlink is not a file. The configured folder itself can be a
  symlink.
- It reads only regular files with the extension `.mkv` or `.mp4`, in any letter case.
- It keeps a file only when its name is the name of one episode and not of a sample. A file with an obfuscated name is
  not kept.
- It reads names only, not the file size or the file content.
- It logs a subfolder that it cannot read, skips it, and keeps the rest of the scan.

### Scan interval

A file that arrives in a source folder counts at the latest one `sourceFolderScanInterval` after it arrived. The next
scan starts one interval after the end of the last scan, also after a long scan at startup. Each scan reads the folder
tree, so the disks of the folders cannot spin down while a scan runs. For a large library on disks that spin down, set
a longer interval, for example `"6h"`.

A changed interval applies at the config reload. The next scan then starts one new interval after the end of the last
scan. When that time is in the past, the next scan starts at once.

### How seasonpackarr uses the files

The candidate check, the match, and the import use the files of the source folders as they use the episodes of the
torrents in the client. A compatible file is hardlinked into the pack folder, and only the missing data downloads.

- The candidate check reads the result of the last scan only. It reads no file on disk.
- A file never gives status `210` (already in client), because only a torrent proves that the client has the pack.
- The match reads each candidate file one time. The file is reused only when it is a regular file with the size of the
  file in the torrent. A file that was removed or replaced after the last scan is dropped with the log line
  `client episode file is not reusable` and the field `source_kind` set to `source_folder`. The coverage rule then
  accepts or rejects the pack.
- When a torrent and a file supply the same episode, the torrent wins.
- [Smart mode](#smart-mode) counts the files of the source folders in the coverage.
- RSS monitoring and search make their search groups from the torrents of the client only, and the search report does
  not count the files. When discovery checks a listing, its match also uses the files of the source folders.

### File names

seasonpackarr reads the season, the episode, and the [release variant](../GLOSSARY.md) of a file from its file name
only. A file gives a hardlink only when its name holds the same release variant as the season pack: the resolution, the
source, the release group, the HDR formats, the streaming service, and the other properties of the
[fuzzy matching](#fuzzy-matching) compare. A name that does not hold them gives no match. This costs an extra download,
never a wrong hardlink.

| Format | Example file name | Result |
| --- | --- | --- |
| Scene style | `Series.Title.S01E02.1080p.WEB-DL.DDP5.1.H.264-GRP.mkv` | Matches a pack such as `Series.Title.S01.1080p.WEB-DL.DDP5.1.H.264-GRP`. |
| TRaSH naming format | `Series Title (2023) - S01E02 - Episode Title [AMZN WEBDL-1080p][DV HDR10][EAC3 5.1][h265]-GRP.mkv` | Matches a pack with the same resolution, source, release group, HDR formats, and streaming service, such as `Series.Title.2023.S01.1080p.AMZN.WEB-DL.DDP5.1.DV.HDR10.H.265-GRP`. For a pack name with no year, see [Years in file names](#years-in-file-names). |
| Default Sonarr format | `Series Title (2023) - S01E02 - Episode Title WEBDL-1080p.mkv` | No match. The name has no release group, and seasonpackarr reads `1080p` as the release group and finds no resolution. Use a naming format with the release group, for example the TRaSH format. |
| Obfuscated name | `a3f9c1e07b2d4e8f9a0b.mkv` | Ignored. The name has no episode. |

### Years in file names

A file name with a year (`Series Title (2023) - S01E02 ...`) is of another season than a pack name with no year
(`Series.Title.S01...`). To let such files match, set
[`fuzzyMatching.skipYearCompare`](#fuzzy-matching) to `true`. The setting is global: it applies to the torrents of
each client too, and not only to the files of source folders.

### One filesystem

A hardlink cannot go from one filesystem to another. Each source folder must be on the filesystem of the import root
of its client. When they are on different filesystems, the import fails for those files, and the error text says that
the hardlink source and the import root are on different filesystems.

With Docker, a hardlink also cannot go from one bind mount to another, even on the same disk. Mount one parent folder
that holds the source folders and the import root, for example `/data`, and use paths below it:

```yaml
volumes:
  - /mnt/storage/data:/data
```

Then use `/data/media/tv` as the source folder and `/data/torrents/tv` as the import root.

### Permissions

Linux protects hardlinks (`fs.protected_hardlinks`): a process can make a hardlink to a file only when it owns the
file, or when it can read and write the file. Thus seasonpackarr must own each file of a source folder, or have read
and write access to it. A file that another user owns with read access only gives a failed hardlink. Run seasonpackarr
with the user or the group of the tool that writes the files, for example the same `PUID` and `PGID` as Sonarr.

### Risks

- **A pack that you removed comes back.** A file stays in its source folder after you remove its torrent from the
  client. When an announce offers the pack again, seasonpackarr imports it again. Remove the files too, or remove their
  folder from the list.
- **A failed data check writes into the source file.** The torrent client checks the hardlinked data. When the check
  fails, the client downloads the bad pieces and writes them through the hardlink into the source file. This can occur
  when a file has the size of the file in the torrent but other data, for example another encode with the same size.
- **No folder for incomplete downloads.** Do not use a folder for incomplete downloads, for example the download folder
  of a Usenet client, as a source folder. A file there can be incomplete or can move at any time.

## Smart mode

Smart mode rejects a season pack when too few of its episodes are already on disk.

| Setting | Default | Meaning |
| --- | --- | --- |
| `smartMode` | `false` | Turns smart mode on. |
| `smartModeThreshold` | `0.75` | The minimum coverage that a pack needs. Greater than `0` and at most `1`. |

Coverage is the share of distinct episode files in the torrent that seasonpackarr can reuse from the client and from its
[source folders](#source-folders). MKV and MP4 episodes count. Samples, extra videos, and files with a different
container do not count. seasonpackarr reads the episode count from the torrent and uses no external episode database.

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
| `fuzzyMatching.skipRepackCompare` | Ignores the repack status. The pack `Show.S01.1080p.WEB-DL.DDPA5.1.H.264-RlsGrp` then matches the episode `Show.S01E01.1080p.WEB-DL.REPACK.DDPA5.1.H.264-RlsGrp`. |
| `fuzzyMatching.simplifyHdrCompare` | Treats `HDR10`, `HDR10+`, and `HDR+` as `HDR`. The pack `Show.S01.2160p.WEB-DL.DDPA5.1.DV.HDR10+.H.265-RlsGrp` then matches the episode `Show.S01E01.2160p.WEB-DL.DDPA5.1.DV.HDR.H.265-RlsGrp`. |
| `fuzzyMatching.simplifyWebCompare` | Treats `WEB-DL` and `WEB` as the same source. |
| `fuzzyMatching.skipYearCompare` | Lets a release without a year match a release with a year. |

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

seasonpackarr sends each notification to every notification destination: the Discord webhook and each Shoutrrr URL.
With no destination, it sends no notifications.

| Setting | Default | Meaning |
| --- | --- | --- |
| `notifications.discord` | empty | Discord webhook URL. Notifications to it are Discord embeds. |
| `notifications.shoutrrr` | `[]` | List of [Shoutrrr URLs](https://shoutrrr.nickfedor.com/latest/services/overview/), for example Gotify, ntfy, Pushover, or Telegram. |
| `notifications.notificationLevel` | `["MATCH", "ERROR"]` | Which events to send. The level applies to all destinations. |

- `MATCH`: an accepted season pack. The import sends one notification for each accepted pack. A successful match sends none.
- `INFO`: a rejected season pack from a match or an import. The candidate check sends no notifications.
- `ERROR`: a failed request, for example a client connection error.

```yaml
notifications:
  notificationLevel: ["MATCH", "ERROR"]
  shoutrrr:
    - "gotify://gotify.example.com/<token>"
    - "ntfy://ntfy.sh/<topic>"
```

A Shoutrrr destination gets plain text: the title, then one line each for the release name, the client, and the action,
and the error for an `ERROR` notification. A `discord://` Shoutrrr URL also sends plain text. For embeds, use
`notifications.discord`. If you set both for the same channel, each notification arrives twice.

seasonpackarr checks each Shoutrrr URL when it loads the config. An invalid URL stops startup, and a config reload with
an invalid URL keeps the last valid config. The error names the list position and the scheme, for example
`notifications.shoutrrr[1] (gotify)`, and never the URL. A failed send to one destination does not stop the others.
Each send has a 30-second timeout, and each failure gets its own log line.

To check the destinations, run:

```sh
seasonpackarr notification test --config <dir>
```

The command reads `config.yaml` and the environment variables as `start` does, but does not write a default config.
It needs no running service and ignores `notificationLevel`. It sends one
test notification to each destination, prints one line for each, for example `discord: ok`, and exits `1` when a send
fails or when no destination is configured.

## Prowlarr discovery

The `search` settings connect seasonpackarr to Prowlarr for RSS monitoring and search. See
[Prowlarr discovery](PROWLARR.md) for how they work.

| Setting | Default | Meaning |
| --- | --- | --- |
| `search.prowlarrURL` | empty | Base URL of Prowlarr, including the URL base when used. Do not append `/api/v1`. |
| `search.apiKey` | empty | Prowlarr API key. |
| `search.indexerIDs` | `[]` | Prowlarr indexer IDs for RSS polls and searches. An empty list selects all eligible indexers. |
| `search.rssInterval` | `"0s"` | Time between RSS polls. `"0s"` turns RSS monitoring off. Otherwise at least `"10m"`. |
| `search.requestInterval` | `"10s"` | Minimum time between requests to Prowlarr. At least `"10s"`. |

## Logging

| Setting | Default | Meaning |
| --- | --- | --- |
| `logLevel` | `"DEBUG"` | `"ERROR"`, `"WARN"`, `"INFO"`, `"DEBUG"`, or `"TRACE"`. |
| `logPath` | empty | Log file. When empty, seasonpackarr logs to stdout. Use forward slashes and include the file name, for example `logs/seasonpackarr.log`. |
| `logMaxSize` | `50` | Maximum log file size in megabytes. At least `1`. |
| `logMaxBackups` | `3` | Number of old log files to keep. At least `0`. `0` keeps all old log files. |

## Environment variables

Each setting has an environment variable with the `SEASONPACKARR__` prefix. Environment variables override
`config.yaml`. The repository's [`docker-compose.yml`](../docker-compose.yml) lists all of them. The variables that few
setups need are commented-out lines.

Variable names follow the rule of the operating system. On Windows, names have no case, so `Seasonpackarr__Host` also
sets `host`. On other systems, write the names in upper case, as this guide shows them.

- Top-level settings: `SEASONPACKARR__HOST`, `SEASONPACKARR__PORT`, `SEASONPACKARR__API_TOKEN`,
  `SEASONPACKARR__LOG_LEVEL`, `SEASONPACKARR__LOG_PATH`, `SEASONPACKARR__LOG_MAX_SIZE`, `SEASONPACKARR__LOG_MAX_BACKUPS`,
  `SEASONPACKARR__SMART_MODE`, `SEASONPACKARR__SMART_MODE_THRESHOLD`, `SEASONPACKARR__SOURCE_FOLDER_SCAN_INTERVAL`.
- Fuzzy matching: `SEASONPACKARR__FUZZY_MATCHING_SKIP_REPACK_COMPARE`,
  `SEASONPACKARR__FUZZY_MATCHING_SIMPLIFY_HDR_COMPARE`, `SEASONPACKARR__FUZZY_MATCHING_SIMPLIFY_WEB_COMPARE`,
  `SEASONPACKARR__FUZZY_MATCHING_SKIP_YEAR_COMPARE`.
- Notifications: `SEASONPACKARR__NOTIFICATIONS_DISCORD`, `SEASONPACKARR__NOTIFICATIONS_NOTIFICATION_LEVEL` as a
  comma-separated list, for example `MATCH,ERROR`, and `SEASONPACKARR__NOTIFICATIONS_SHOUTRRR` as a list separated by
  spaces, for example `"gotify://gotify.example.com/<token> ntfy://ntfy.sh/<topic>"`. Shoutrrr URLs use whitespace, not
  commas, because a URL can hold commas, for example Telegram `chats=a,b`.
- Clients: `SEASONPACKARR__CLIENTS_<NAME>_<SETTING>`. `<NAME>` is the client name in upper case. The client name
  becomes lower case. In these variables, a client name cannot contain an underscore.
  - Connection: `SEASONPACKARR__CLIENTS_<NAME>_TYPE`, `SEASONPACKARR__CLIENTS_<NAME>_HOST`,
    `SEASONPACKARR__CLIENTS_<NAME>_PORT`, `SEASONPACKARR__CLIENTS_<NAME>_USERNAME`,
    `SEASONPACKARR__CLIENTS_<NAME>_PASSWORD`, `SEASONPACKARR__CLIENTS_<NAME>_APIKEY`.
  - Import policy: `SEASONPACKARR__CLIENTS_<NAME>_IMPORT_SAVE_PATH`, `SEASONPACKARR__CLIENTS_<NAME>_IMPORT_CATEGORY`,
    `SEASONPACKARR__CLIENTS_<NAME>_IMPORT_DOWNLOAD_PATH`, `SEASONPACKARR__CLIENTS_<NAME>_IMPORT_CONTENT_LAYOUT`, and
    `SEASONPACKARR__CLIENTS_<NAME>_IMPORT_TAGS` as a comma-separated list.
  - Inventory scope: `SEASONPACKARR__CLIENTS_<NAME>_INVENTORY_INCLUDE_CATEGORIES`,
    `SEASONPACKARR__CLIENTS_<NAME>_INVENTORY_INCLUDE_TAGS`, `SEASONPACKARR__CLIENTS_<NAME>_INVENTORY_EXCLUDE_CATEGORIES`,
    and `SEASONPACKARR__CLIENTS_<NAME>_INVENTORY_EXCLUDE_TAGS`, each as a comma-separated list. Without an `INVENTORY_`
    variable, the scope of `config.yaml` applies.
  - Source folders: `SEASONPACKARR__CLIENTS_<NAME>_SOURCE_FOLDERS` as a comma-separated list of absolute paths.
- Prowlarr: see [Prowlarr discovery](PROWLARR.md).

At startup, seasonpackarr logs a warning for each `SEASONPACKARR__` variable that it does not know, for example
`SEASONPACKARR__LOG_LEVL` or `SEASONPACKARR__CLIENTS_DEFAULT_IMPORT_SAVEPATH`. An unknown variable does not stop startup,
and seasonpackarr ignores its value. The variables of the command-line commands (`SEASONPACKARR__URL`,
`SEASONPACKARR__CLIENT`, `SEASONPACKARR__AUTOBRR_URL`, and `SEASONPACKARR__AUTOBRR_API_KEY`) give no warning.

seasonpackarr ignores an environment variable with an empty value. The variables that v0.16.0 used for removed settings are
the exception: they stop startup even when they are empty. See
[Docker Compose and environment variables](UPGRADE-v1.md#docker-compose-and-environment-variables).

### Bad values

A value that has no valid meaning stops the load. This rule applies to `config.yaml` and to the environment variables.
The error names the setting or the variable. It does not show the value, because the value can be a secret.

- A boolean variable must be `true` or `false` (`1`, `0`, `t`, and `f` also work).
- An integer or number variable must be a number, for example `SEASONPACKARR__PORT=abc` stops the load.
- A text variable must not contain a control character, such as a line break at the end of a value from a secret file.
- seasonpackarr splits a list variable on its separator (commas, or whitespace for Shoutrrr URLs), trims each entry, and
  drops empty entries. When no entry is left, such as `" "` or `","`, the load stops, because the variable would
  replace the list of `config.yaml` with an empty list. For an `INVENTORY_` variable, an empty scope reads every
  torrent.
- After seasonpackarr merges `config.yaml` and the environment variables, it checks the ranges: `port` from `1` to
  `65535`, `logMaxSize` at least `1`, `logMaxBackups` at least `0`, `smartModeThreshold` greater than `0` and at
  most `1`, `sourceFolderScanInterval` a duration of at least `1m`, and `clients.<name>.port` from `0` to `65535`. The
  ranges apply to both sources.
- Each client must have a `host` that its client type can connect to. An empty `host` stops the load. A Deluge `host`
  must be a hostname or IP address, so a URL scheme (`http://`), user info (`user@`), or a path stops the load. For
  qBittorrent and Transmission, a `host` that does not parse as a URL, a URL with no host name such as
  `http://:8080`, or a port in `host` outside `1` to `65535` with no `port` setting stops the load. The error names
  the client, but it does not show the `host`, because a path or user info in it can be a secret.

The commands that call the running service, such as `candidate` and `setup filter`, read only the host, the API token,
and the client names. A bad server-only variable, for example `SEASONPACKARR__SMART_MODE=yes`, does not stop them.

To run without `config.yaml`, set `SEASONPACKARR__DISABLE_CONFIG_FILE=true` and configure everything with environment
variables. The database then moves, see [Database location](INSTALL.md#database-location). This variable accepts the
same values as all other boolean variables. `true` and `1` disable `config.yaml`. `false`, `0`, and an empty value read
`config.yaml`. Other forms, such as `TRUE`, `t`, and `f`, also work. A different value, such as `yes`, stops the start
and each command that reads the config. The error names the variable.

## Config reload

seasonpackarr watches the active `config.yaml` while it runs. For each change, it loads the complete file, applies the
environment variables again, and validates the result. Only a valid result becomes active. When the file is not valid,
for example during a partial write, with an [unknown key](#unknown-keys), or with a [bad value](#bad-values),
seasonpackarr logs the error and keeps the last valid config.

These settings apply without a restart:

- torrent client connections, import policies, inventory scopes, and source folders
- API token
- smart mode and fuzzy matching
- notification destinations and notification levels
- log level
- Prowlarr connection, request spacing, and RSS interval
- source folder scan interval

These settings need a restart:

- `host` and `port`
- `logPath`, `logMaxSize`, and `logMaxBackups`
- `SEASONPACKARR__DISABLE_CONFIG_FILE`

When a reload removes a client or changes its connection, import, inventory, or source folder settings, seasonpackarr
drops the cached connection and inventory of that client. The next request connects again. A new source folder gets a
scan at once, and the scan result of a folder that no client names is dropped.

When you remove an optional setting, it returns to its default. A running search or RSS run keeps the config it started
with. A changed RSS interval sets the time of the next automatic run.
