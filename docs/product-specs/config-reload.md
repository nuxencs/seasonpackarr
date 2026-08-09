# Config Reload

## Operator Promise

seasonpackarr watches the active `config.yaml` file while the service runs. A reload must not expose a partial or
invalid config to requests.

For each file change, seasonpackarr:

1. starts from built-in defaults
2. loads the complete YAML file
3. reapplies `SEASONPACKARR__...` environment overrides
4. validates deprecated settings and every torrent client
5. atomically publishes the new immutable snapshot

If any step fails, seasonpackarr logs the rejection and keeps the last valid snapshot.

## Live Settings

The following settings apply without a restart:

- torrent client connections and import policies
- API token
- smart mode and fuzzy matching
- Discord webhook and notification levels
- log level

Each request or notification uses one coherent snapshot. Cached inventories and import plans validate the relevant
client and matching settings before reuse. Changed settings cause the next request to rebuild stale data.

## Restart-Only Settings

The following settings configure components that start once:

- server host and port
- log path, maximum size, and backup count
- `disableConfigFile`

These changes require a process restart.

## Acceptance Bar

- invalid YAML or invalid client settings keep the last valid config active
- truncate-first file writes never publish an empty intermediate snapshot
- environment values keep precedence after every reload
- omitted optional keys return to built-in defaults
- concurrent readers do not race with reload publication
- automatic config discovery watches the file that was actually loaded
