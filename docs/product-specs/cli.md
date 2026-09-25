# Command-Line Guide

Use the CLI to check a release, verify exact episode reuse, or import a pack.
The service must be running for `candidate`, `match`, `import`, and `search`.

## First Successful Check

Start with the [new-user setup](new-user-onboarding.md). Configure a torrent
client and `apiToken`, then start the service:

```sh
seasonpackarr start --config ~/.config/seasonpackarr
```

In another terminal, check a representative release name:

```sh
seasonpackarr candidate "Series.S01.1080p.WEB-DL-GRP"
```

Replace the example name with an actual season pack. The command reads the
connection settings from your config and selects the sole configured client.
With several clients, add `--client <name>`. If the service uses a different
config directory, add `--config <directory>` to each API command.

An accepted candidate means that the release name is compatible with episodes
in the client. It does not prove that the torrent files can reuse those episodes.

Download the original `.torrent` file from your tracker, then check exact reuse:

```sh
seasonpackarr match ./pack.torrent
```

By default, the command reads the release name from the torrent contents.
You do not need to rename the downloaded file. It checks the pack without
creating hardlinks or adding a torrent. An accepted result has this form:

```text
Match accepted: exact reuse checks passed
Client: tv
Release: Series.S01.1080p.WEB-DL-GRP
```

A rejected result gives the reason, such as insufficient matching episodes.
The service logs contain detailed per-episode diagnostics.

To create hardlinks and import the pack into the torrent client, run:

```sh
seasonpackarr import ./pack.torrent
```

### Torrents With A Short Folder Name

Some torrents use a folder name such as `Series.S01` that omits the quality,
source, or release group. For these torrents, supply the full tracker release
name with `--release`:

```sh
seasonpackarr match ./pack.torrent --release "Series.S01.1080p.WEB-DL-GRP"
```

Use the same `--release` value with `import`. This option changes the name used
for release checks. It does not change the torrent contents or destination
folder name. Match and import still require the original `.torrent` file.

## Commands And Required Inputs

| Command | Required input | Effect |
| --- | --- | --- |
| `candidate` | Quoted release name | Checks release-name compatibility; does not request torrent metadata |
| `match` | Real `.torrent` file | Checks exact reuse; does not create hardlinks or import |
| `import` | Real `.torrent` file | Creates hardlinks and imports the pack |
| `search --dry-run` | None | Searches Prowlarr and checks release names |
| `search --dry-run --verify` | None | Searches Prowlarr and checks exact reuse |
| `search` | None | Searches Prowlarr and imports accepted packs |
| `start` | Config directory if outside the default locations | Starts the service; an explicit directory gets a default config if missing |
| `gen-token` | None | Generates a token to store in `apiToken` |
| `version` | None | Prints local build information without network access |
| `completion` | Shell name | Generates shell completion; see `completion --help` |

Search requires a configured Prowlarr connection. It selects all clients unless
`--client` or `SEASONPACKARR__CLIENT` selects one. See
[Prowlarr discovery](prowlarr-backfill.md#manual-runs) for modes and limits.

## Connection Settings

All four API commands accept the same flags. Explicit flags override environment
settings, which override local config, which overrides built-in defaults.

| Setting | Flag | Environment variable | Config or default |
| --- | --- | --- | --- |
| Config directory | `--config`, `-c` | None | Discovery locations below |
| Service base URL | `--url` | `SEASONPACKARR__URL` | HTTP URL built from host and port |
| Service host | `--host`, `-i` | `SEASONPACKARR__HOST` | `host`, otherwise loopback |
| Service port | `--port`, `-p` | `SEASONPACKARR__PORT` | `port`, otherwise `42069` |
| API token | `--api`, `-a` | `SEASONPACKARR__API_TOKEN` | `apiToken`, otherwise empty |
| Client name | `--client`, `-n` | `SEASONPACKARR__CLIENT` | Sole configured client; without local clients, `default`; search selects all |

The token belongs to seasonpackarr, not Prowlarr or the torrent client.
`SEASONPACKARR__URL` and `SEASONPACKARR__CLIENT` are CLI-only environment settings,
not YAML keys. Client names also include clients defined through the existing
`SEASONPACKARR__CLIENTS_...` environment settings.

Config discovery checks these locations in order:

1. `./config.yaml` in the current working directory.
2. `$HOME/.config/seasonpackarr/config.yaml`.
3. `$HOME/.seasonpackarr/config.yaml`.

API commands only read config. They do not create files, start a service, or
require valid server-only settings such as import policies. An unreadable or
invalid existing config is an error. A missing implicit config permits remote
use with flags and environment settings. An explicit `--config` directory must
contain `config.yaml`. `SEASONPACKARR__DISABLE_CONFIG_FILE=true` skips file loading,
including an explicit directory.

Listener addresses `0.0.0.0` and `::` become `127.0.0.1` and `::1` for local
requests. Use `--url` for HTTPS or a reverse-proxy path. Do not combine it with
`--host` or `--port`. Explicit host/port flags override an environment URL;
the other host/port component still comes from environment, config, or defaults.
URLs must not contain credentials, query parameters, or fragments. Redirects
are rejected; supply the final service URL.
When a URL supplies the address, unused host and port values are ignored.

For repeated remote use, set the connection once in your shell environment:

```sh
export SEASONPACKARR__URL="https://example.com/seasonpackarr"
export SEASONPACKARR__API_TOKEN="your-seasonpackarr-api-token"
export SEASONPACKARR__CLIENT="tv"
seasonpackarr match ./pack.torrent
```

Replace the URL, token, and client with your service settings. To search all
clients despite an environment client setting, use `search --client ""`.

## Results, Errors, And Scripts

Human-readable results are the default. Add `--json` for structured results:

```sh
seasonpackarr match ./pack.torrent --json
seasonpackarr search --dry-run --json
```

Check/import JSON includes `operation`, `status`, `statusCode`, `clientname`,
`name`, and `message`. Search JSON preserves the full API report. Result data
goes to stdout. Input, connection, and response-decoding errors go to stderr.
Search progress also goes to stderr. A failed API operation
can still produce a result on stdout; always inspect the exit code.
An unexpected HTTP 200 response, such as a proxy login page, returns exit code
`1` and reports an invalid service response.

| Exit code | Meaning |
| --- | --- |
| `0` | Operation accepted, or search completed without operational failures |
| `1` | Invalid input, configuration, connection, authentication, or operation failure |
| `2` | Candidate, match, or import rejected the pack, including an existing pack |

Search rejections and empty results are normal outcomes and return `0`. Tracker,
client, or import failures return `1`, including partial failures. Search stays
connected until completion. Ctrl+C cancels its request. Cancellation of an import
can leave hardlinks or an added torrent; inspect the client before retrying.
Candidate, match, and import requests have a 30-second timeout. An import can
continue on the service after a timeout if an external operation cannot cancel.

## Migration From `test`

V1 removes the entire `test` command group. Replace `test candidate`,
`test match`, and `test import` with `candidate`, `match`, and `import`.
No aliases remain. For the v0.16.0 commands `test pack` and `test parse`, see the
[v1 upgrade guide](v1-upgrade.md#update-cli-commands-and-scripts).

Match and import now require a real torrent file. Release-name inputs previously
created mock torrents with five artificial episodes. They could not establish
real reuse. Use `candidate` for a release-name check.

Search now prints readable text by default. Add `--json` to scripts that consume
the previous JSON output. Commands now return failure exit codes for errors that
previously printed a message and returned success. `version` no longer queries
GitHub for the latest release.
