# Command-line guide

Use the CLI to run a candidate check on a release name, match a torrent, or
import a season pack. The service must be running for `candidate`, `match`,
`import`, and `search`. Use `setup filter` to create the autobrr filter.

## First successful check

Start with the [installation guide](INSTALL.md). Configure a torrent client and
`apiToken`, then start the service:

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
config directory, add `--config <directory>` to each API command. In Docker,
run the command in the container, for example
`docker exec seasonpackarr seasonpackarr candidate "<release>"`. The command
finds `/config/config.yaml` without `--config`.

An accepted candidate check means that the release name is compatible with
episodes in the client. It does not prove that the torrent files can reuse those
episodes.

Download the original `.torrent` file from your tracker, then run a match:

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

### Torrents with a short folder name

Some torrents use a folder name such as `Series.S01` that omits the quality,
source, or release group. For these torrents, supply the full tracker release
name with `--release`:

```sh
seasonpackarr match ./pack.torrent --release "Series.S01.1080p.WEB-DL-GRP"
```

Use the same `--release` value with `import`. This option changes the name used
for release checks. It does not change the torrent contents or destination
folder name. Match and import still require the original `.torrent` file.

## Commands and required inputs

| Command | Required input | Effect |
| --- | --- | --- |
| `candidate` | Quoted release name | Runs a candidate check; does not request torrent metadata |
| `match` | Real `.torrent` file | Runs a match; does not create hardlinks or import |
| `import` | Real `.torrent` file | Creates hardlinks and imports the pack |
| `search --dry-run` | None | Searches Prowlarr and runs candidate checks |
| `search --dry-run --verify` | None | Searches Prowlarr and runs matches |
| `search` | None | Searches Prowlarr and imports accepted packs |
| `setup filter` | `--webhook-url`, autobrr URL, and autobrr API key | Creates one disabled autobrr filter for each client, or updates the filter from `--filter-id`; see [Set up the autobrr filter](#set-up-the-autobrr-filter) |
| `start` | Config directory if outside the default locations | Starts the service; an explicit directory gets a default config if missing |
| `notification test` | Config directory if outside the default locations | Sends a test notification to each notification destination; see [Notifications](CONFIGURATION.md#notifications) |
| `gen-token` | None | Generates a token to store in `apiToken` |
| `version` | None | Prints local build information without network access |
| `completion` | Shell name | Generates shell completion; see `completion --help` |

Search requires a configured Prowlarr connection. It selects all clients unless
`--client` or `SEASONPACKARR__CLIENT` selects one. See
[Prowlarr search](PROWLARR.md#search) for modes and limits.

## Set up the autobrr filter

`setup filter` writes the seasonpackarr filter into autobrr through the autobrr
REST API. The filter comes from the binary, so it always matches the version
that answers its requests. The command does not call the service itself, but
its [verification](#verify-the-filter) needs a running service.

```sh
export SEASONPACKARR__AUTOBRR_URL="http://autobrr:7474"
export SEASONPACKARR__AUTOBRR_API_KEY="your-autobrr-api-key"
seasonpackarr setup filter --webhook-url http://seasonpackarr:42069
```

Replace the URLs and the key with your values. Create the API key in autobrr
under **Settings**, **API keys**. The environment variables keep the key out of
your shell history.

The command creates one filter for each configured client, or only the filter
for the client from `--client` or `SEASONPACKARR__CLIENT`. Each filter is named
`seasonpackarr-<client>` and has the External entries and the import action of
the [autobrr guide](AUTOBRR.md). A new filter is disabled and has no indexers.
After the command, do these steps in autobrr for each new filter:

1. Select the indexers in **General** > **Indexers**.
2. Set the priority higher than your other TV filters.
3. Enable the filter.

If a filter named `seasonpackarr-<client>` already exists for a selected
client, the command writes no filter. It prints the ID of each existing filter
and `rerun with --filter-id <id> --client <client>`, so a rerun never gives a
client a second filter.

### Update an existing filter

Use `--filter-id` to update a filter that you already have, for example a
filter from v0.16.0. Find the ID in the URL of the filter in autobrr, for
example `/filters/12`.

```sh
seasonpackarr setup filter --webhook-url http://seasonpackarr:42069 --client tv --filter-id 12
```

The update replaces the External entries and all actions of the filter. It
keeps all other settings: the name, the indexers, the resolutions, the
priority, the enabled state, and the notifications. The command prints each
action that it removes, for example the old torrent client action, because the
import action adds the pack to the client.

`--filter-id` needs exactly one client: the client from `--client` or
`SEASONPACKARR__CLIENT`, or the only configured client. Otherwise the command
fails before it calls autobrr, because one filter has one `clientname`.

| Input | Flag | Environment variable | Config or default |
| --- | --- | --- | --- |
| seasonpackarr URL that autobrr uses | `--webhook-url` | None | Required, no default |
| autobrr base URL | `--autobrr-url` | `SEASONPACKARR__AUTOBRR_URL` | Required, except with `--dry-run` without `--filter-id` |
| autobrr API key | `--autobrr-api-key` | `SEASONPACKARR__AUTOBRR_API_KEY` | Required, except with `--dry-run` without `--filter-id` |
| Config directory | `--config`, `-c` | None | Discovery locations below |
| seasonpackarr API token | `--api`, `-a` | `SEASONPACKARR__API_TOKEN` | `apiToken`, otherwise empty |
| Client name | `--client`, `-n` | `SEASONPACKARR__CLIENT` | Each configured client |
| Filter to update | `--filter-id` | None | None, the command creates filters |

`--webhook-url` is the address of seasonpackarr as autobrr sees it, for
example `http://seasonpackarr:42069` when both run in Docker. It follows the
rules of `--url`: HTTP or HTTPS, a host, an optional proxy path, and no
credentials, query parameters, or fragment.

The command works without a config file. Then give the client and the token
with `--client` and `--api`. With an empty token, the command prints a warning
and writes a filter without the `X-API-Token` header and without the `apikey`
query parameter. The token must not contain `;` or `=`, because autobrr cannot
send such a token in a header. Tokens from `gen-token` are always valid.

Add `--dry-run` to print each filter with the tokens redacted. A dry run makes
no autobrr calls, so it needs no autobrr URL or key. With `--filter-id`, a dry
run reads the filter from autobrr and writes nothing. It prints the External
entries and the actions that the update sends, and the actions that the update
removes. Add `--json` for structured output. Each result has `clientname`,
`id`, `name`, and `result`: `created`, `updated`, or `dry-run` with the
`filter` payload. An update and its dry run also have `removed_actions`, with
the `id`, `name`, and `type` of each action. If the search finds old filters,
each result also has `stale_filters`, with the `id`, `name`, and `endpoints` of
each filter. A created or updated filter also has `verification`, with the
`result` (`passed`, `failed`, or `skipped`), the HTTP `status` that autobrr got
from the webhook URL, and a `message`. See
[Verify the filter](#verify-the-filter). Results go to stdout, and warnings go
to stderr.

| Exit code | Meaning |
| --- | --- |
| `0` | Each filter is created or updated and its verification passed or was skipped, or the dry run printed each filter. Warnings do not change the exit code |
| `1` | Invalid input, an existing filter with the same name, an autobrr API error, or a failed verification |

After an autobrr error, the command stops and prints the filters that it
created before the error. The update removes the old actions before it writes
the filter. If the write fails after that, the filter has no action and
downloads nothing until you rerun the command. After the update, the command
reads the filter again. If autobrr still has an old action, the command fails
and names each action, so that you can delete the old actions in autobrr.

### Verify the filter

After each create or update, the command asks autobrr to run the candidate
check entry of the filter through the autobrr External test endpoint. autobrr
sends the request, so the check uses the network position of autobrr and the
`--webhook-url` of the filter. autobrr sends a fixed single-episode sample
release. seasonpackarr checks the client before the release name, so a pass is
usually a rejection such as status `200` or `211`. The text output shows the
result below each filter:

```text
Created filter 1 (seasonpackarr-tv) for client tv
  Candidate check through autobrr: passed (status 211): the check reached seasonpackarr
```

| Result of the check | Cause in the message | Outcome |
| --- | --- | --- |
| No connection | autobrr cannot reach the webhook URL | Fail |
| `401` | seasonpackarr rejected the token | Fail |
| `472` | seasonpackarr does not know the client | Fail |
| `471` or `468` | seasonpackarr cannot reach the torrent client | Fail |
| A different answer that is not a seasonpackarr status, for example a `404` from a proxy | The webhook URL does not point to seasonpackarr | Fail |
| A different seasonpackarr status, for example `211` | The check reached seasonpackarr | Pass |
| `404` from autobrr | autobrr has no External test endpoint, because it is older than v1.88.0 | Skip with a warning |

A failed verification does not remove the filter. The command writes the
filters for all selected clients, prints each result, and exits with `1`. The
error names each filter that failed and says that the filter is written. Fix
the cause, then rerun the command with `--filter-id <id> --client <client>` to
write and verify the filter again. A skipped verification prints a warning and
keeps exit code `0`. A dry run writes nothing, so it does not verify.

### Find old filters

Each run that calls autobrr also searches the other filters for filters that
still call seasonpackarr, for example a v0.16.0 filter that calls `/api/pack`.
The search reads each filter and finds each External entry or action with a
webhook URL that contains `/api/pack`, `/api/parse`, `/api/candidate`,
`/api/match`, or `/api/import`. For each filter that it finds, the command
prints a warning with the ID, the name, and the endpoints:

```text
Warning: filter 7 (TV packs) calls seasonpackarr at /api/pack; update it with --filter-id 7 --client <client>
```

The command never changes these filters. To update one, rerun the command with
its ID in `--filter-id` and the client of that filter in `--client`. The search also finds the current filter of a client
that you did not select, because that filter calls seasonpackarr too. The
search skips the filter from `--filter-id`. A dry run without `--filter-id`
makes no autobrr calls, so it does not search. The warnings keep exit code `0`.

## Connection settings

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
`SEASONPACKARR__URL`, `SEASONPACKARR__CLIENT`, `SEASONPACKARR__AUTOBRR_URL`,
and `SEASONPACKARR__AUTOBRR_API_KEY` are CLI-only environment settings, not YAML
keys. Client names also include clients defined through the existing
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

## Results, errors, and scripts

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

## Upgrade from v0.16.0

v1.0.0 removes the `test` command group, and `match` and `import` now require a
real `.torrent` file. Search prints readable text unless you add `--json`, and
errors now return a failure exit code. See
[Update scripts that use the CLI](UPGRADE-v1.md#3-update-scripts-that-use-the-cli)
for the command mapping.
