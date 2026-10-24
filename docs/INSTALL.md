# Install

This guide installs seasonpackarr, creates its first configuration, and explains where it keeps its data.

Upgrading from v0.16.0? Read the [v0.16.0 to v1.0.0 upgrade guide](UPGRADE-v1.md) first.

## Requirements

- A supported torrent client: qBittorrent, Transmission, or Deluge. See [Torrent clients](CONFIGURATION.md#torrent-clients)
  for the supported versions.
- At least one way to find season packs: [autobrr](AUTOBRR.md), [Prowlarr](PROWLARR.md), or both. Without them, you can
  still start searches on demand with the [command line](CLI.md).
- seasonpackarr must see the downloaded episodes at the same paths as the torrent client. Hardlinks work only inside one
  filesystem, so the episodes and the season pack destination must be on the same filesystem.

## Linux binary

1. Download the latest release for your architecture. This example gets the `linux_x86_64` archive:

   ```bash
   curl -s https://api.github.com/repos/nuxencs/seasonpackarr/releases/latest | grep download | grep linux_x86_64 | cut -d\" -f4 | xargs curl -LO
   ```

   You can also download an archive from the [releases page](https://github.com/nuxencs/seasonpackarr/releases/latest),
   or build the source with `go build`.

2. Extract the binary to `/usr/bin` and make it executable. Run these commands as `root` or with `sudo`:

   ```bash
   tar -C /usr/bin -xzf seasonpackarr*.tar.gz
   chmod +x /usr/bin/seasonpackarr
   ```

   Without root access, extract it to a directory in your home folder, for example `~/.bin`.

### Run as a systemd service

A systemd service restarts seasonpackarr after failures and starts it at boot.

1. Create `/etc/systemd/system/seasonpackarr@.service` with this content. The repository has the same file in
   [`distrib/systemd`](../distrib/systemd/seasonpackarr@.service).

   ```systemd
   [Unit]
   Description=seasonpackarr service for %i
   After=syslog.target network-online.target

   [Service]
   Type=simple
   User=%i
   Group=%i
   ExecStart=/usr/bin/seasonpackarr start --config=/home/%i/.config/seasonpackarr

   [Install]
   WantedBy=multi-user.target
   ```

2. Enable and start the service for your user:

   ```bash
   sudo systemctl enable -q --now seasonpackarr@$USER
   ```

3. The first start creates `~/.config/seasonpackarr/config.yaml` and then stops, because the default client settings are
   not complete. Continue with [First configuration](#first-configuration).

4. After you edit the config, restart the service and check that it is **active**:

   ```bash
   sudo systemctl restart seasonpackarr@$USER
   sudo systemctl status seasonpackarr@$USER
   ```

## Docker

The image is `ghcr.io/nuxencs/seasonpackarr`. The repository's [`docker-compose.yml`](../docker-compose.yml) is a
complete example.

```yaml
services:
  seasonpackarr:
    container_name: seasonpackarr
    image: ghcr.io/nuxencs/seasonpackarr
    restart: unless-stopped
    user: ${PUID}:${PGID}
    ports:
      - "127.0.0.1:42069:42069"
    volumes:
      - ${DOCKERCONFDIR}/seasonpackarr:/config
      - /data/torrents:/data/torrents
```

- `/config` holds `config.yaml` and the database. The container always starts with `--config /config`.
- Mount your torrent data at the **same path** that the torrent client uses. seasonpackarr reads episode paths from the
  client and creates hardlinks at those paths. If the client sees `/data/torrents/tv`, seasonpackarr must also see
  `/data/torrents/tv`.
- Run the container as the user that owns the torrent data, with `user: ${PUID}:${PGID}`, so it can create hardlinks.
- autobrr reaches seasonpackarr through the Docker network, for example `http://seasonpackarr:42069`, when both
  containers share a network.

Start the container once. It creates `/config/config.yaml` and stops, because the default client settings are not
complete. Edit the file as described in [First configuration](#first-configuration), then start the container again.

You can also configure the container with environment variables only. See
[Environment variables](CONFIGURATION.md#environment-variables).

## First configuration

The first start writes a commented `config.yaml`. Open it and set these values:

1. **API token.** Generate a token and set it as `apiToken`:

   ```bash
   seasonpackarr gen-token
   ```

   autobrr and the command line send this token with each request. Without `apiToken`, the API has no authentication.

2. **One torrent client.** Edit the `default` entry under `clients`: `type`, `host`, `port`, the credentials, and the
   `import` block. A qBittorrent client must set `import.savePath` or `import.category`, and the generated config sets
   neither, so startup fails until you set one. See [Torrent clients](CONFIGURATION.md#torrent-clients) and
   [Import policy](CONFIGURATION.md#import-policy).

3. **Host and port.** The defaults are `0.0.0.0` and `42069`. Change them only if you need to.

Restart seasonpackarr. A healthy service answers `OK` on its readiness endpoint:

```bash
curl http://localhost:42069/api/healthz/readiness
```

Then choose how seasonpackarr finds season packs:

- [autobrr setup](AUTOBRR.md): check each announce from your trackers.
- [Prowlarr discovery](PROWLARR.md): RSS monitoring and targeted searches through Prowlarr.

You can use one of them or both. Each one works without the other.

## Application data

seasonpackarr keeps its discovery progress in a SQLite database, `seasonpackarr.db`. It creates and upgrades the database
automatically. You do not install SQLite or configure a database. The database exists even when you do not use
Prowlarr.

The database holds discovery state only. It does not hold import jobs, the client inventory, or accept decisions. A
restart does not continue an interrupted import.

### Database location

| Setup | Location |
| --- | --- |
| With a config file | `seasonpackarr.db` beside the active `config.yaml` |
| Docker | `/config/seasonpackarr.db`, in the `/config` volume |
| `SEASONPACKARR__DISABLE_CONFIG_FILE=true` with `--config <dir>` | `<dir>/seasonpackarr.db` |
| `SEASONPACKARR__DISABLE_CONFIG_FILE=true` without `--config` | `$XDG_DATA_HOME/seasonpackarr/seasonpackarr.db`, or `~/.local/share/seasonpackarr/seasonpackarr.db` when `XDG_DATA_HOME` is not set |

### Storage rules

- The service account must be able to write the directory and the database. A read-only directory stops startup.
- Use local storage, not an NFS or SMB share.
- Run only one seasonpackarr instance for each database.
- On Unix, only the owner can open the database. On Windows, restrict the directory to the service account.
- Torrent metadata in the database can contain tracker credentials. Protect database backups as you protect
  `config.yaml`.

SQLite uses write-ahead logging (WAL). While seasonpackarr runs, `seasonpackarr.db-wal` and `seasonpackarr.db-shm` exist
beside the database. Committed data can still be in the WAL file, so keep these files with the database. SQLite
manages checkpoints automatically, and seasonpackarr removes expired records itself. No manual maintenance is
necessary.

### Startup logs

At `INFO` level, messages with `module=database` show the database path. `database ready` confirms that the database
works and reports the schema version and journal mode. It appears at every start.

When an upgrade needs migrations, the logs show the old and new schema versions and the number of migrations.
`database migrations applied` appears after they commit.

### Back up and restore

To make a backup:

1. Stop seasonpackarr cleanly.
2. Copy `config.yaml` and `seasonpackarr.db` to a protected location.
3. Start seasonpackarr again.

A clean stop writes the WAL into the database and removes the `-wal` and `-shm` files. If those files remain after the
stop, the database file alone is not a complete backup. Start seasonpackarr so that SQLite can recover, stop it cleanly,
then make the backup. Do not copy the database while seasonpackarr runs.

To restore, stop seasonpackarr, restore both files with permissions for the service account, then start it.

### Database errors

An unreadable or corrupt database, or a database from a newer seasonpackarr version, stops startup with an error.
seasonpackarr never erases the database and never falls back to an in-memory database.

1. Check the directory permissions and the free disk space.
2. For a corrupt database, restore a backup.
3. For a newer schema, use a compatible seasonpackarr version, or restore the backup from before the upgrade.

A database failure during discovery stops that run. The run's report and the service logs show the error. Imports that
finished before the failure stay in place.

## Next steps

- [Configuration](CONFIGURATION.md): clients, import policy, smart mode, and fuzzy matching.
- [autobrr setup](AUTOBRR.md) or [Prowlarr discovery](PROWLARR.md).
- [Command line](CLI.md): check a release name or a torrent file before you automate.
