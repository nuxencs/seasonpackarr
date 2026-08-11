<h1 align="center">seasonpackarr</h1>
<h1 align="center">
  <a href="https://github.com/nuxencs/seasonpackarr/blob/develop/LICENSE">
    <img src="https://img.shields.io/github/license/nuxencs/seasonpackarr?style=flat-square&color=00ACD7" alt="License">
  </a>
  <a href="https://goreportcard.com/report/github.com/nuxencs/seasonpackarr">
    <img src="https://goreportcard.com/badge/github.com/nuxencs/seasonpackarr?style=flat-square" alt="Go Report">
  </a>
  <a href="https://github.com/nuxencs/seasonpackarr/actions">
    <img src="https://img.shields.io/github/actions/workflow/status/nuxencs/seasonpackarr/release.yml?style=flat-square&logo=github" alt="Build">
  </a>
    <a href="https://github.com/nuxencs/seasonpackarr/releases">
    <img src="https://img.shields.io/github/v/release/nuxencs/seasonpackarr?style=flat-square&color=00ACD7" alt="Latest Release">
  </a>
  <a href="https://trash-guides.info/discord">
    <img src="https://img.shields.io/discord/492590071455940612?style=flat-square&logo=discord&logoColor=00ACD7&label=support&color=00ACD7" alt="Discord">
  </a>
</h1>

<p align="center">
<b>seasonpackarr</b> is a companion app for <a href="https://github.com/autobrr/autobrr">autobrr</a> that automagically <b>hardlinks</b> downloaded episodes into a season folder when a season pack is
announced and adds the pack back to your torrent client, eliminating the need for re-downloading existing episodes.
</p>

> [!WARNING]
> This application is currently under active development. If you encounter any bugs, please report them in the dedicated
> #seasonpackarr channel on the TRaSH-Guides [Discord server](https://trash-guides.info/discord) or create a new issue
> on GitHub, so I can fix them.

> [!IMPORTANT]
> Upgrading from v0.16.0? The configuration and autobrr webhook setup changed. Follow the
> [v0.16.0 to v1.0.0 upgrade guide](docs/product-specs/v1-upgrade.md) before replacing the service.

## Installation

### Linux

To download the latest release, you can use one of the following methods:

```bash
# using curl
curl -s https://api.github.com/repos/nuxencs/seasonpackarr/releases/latest | grep download | grep linux_x86_64 | cut -d\" -f4 | xargs curl -LO

# using wget
wget -qO- https://api.github.com/repos/nuxencs/seasonpackarr/releases/latest | grep download | grep linux_x86_64 | cut -d\" -f4 | xargs wget
```

Alternatively, you can download the [source code](https://github.com/nuxencs/seasonpackarr/releases/latest) and build it yourself using `go build`.

#### Unpack

Run with `root` or `sudo`. If you do not have root, or are on a shared system, place the binary somewhere in your home
directory like `~/.bin`.

```bash
tar -C /usr/bin -xzf seasonpackarr*.tar.gz
```

This will extract `seasonpackarr` to `/usr/bin`.

Afterwards you need to make the binary executable by running the following command.

```bash
chmod +x /usr/bin/seasonpackarr
```

Note: If the commands fail, prefix them with `sudo ` and run them again.

#### Systemd (Recommended)

On Linux-based systems, it is recommended to run seasonpackarr as a sort of service with auto-restarting capabilities,
in order to account for potential downtime. The most common way is to do it via systemd.

You will need to create a service file in `/etc/systemd/system/` called `seasonpackarr@.service`.

```bash
touch /etc/systemd/system/seasonpackarr@.service
```

Then place the following content inside the file (e.g. via nano/vim/ed):

```systemd title="/etc/systemd/system/seasonpackarr@.service"
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

Start the service. Enable will make it startup on reboot.

```bash
sudo systemctl enable -q --now seasonpackarr@$USER
```

Make sure it's running and **active**.

```bash
sudo systemctl status seasonpackarr@$USER
```

On first run it will create a default config, `~/.config/seasonpackarr/config.yaml` that you will need to edit.

After the config is edited you need to restart the service.

```bash
sudo systemctl restart seasonpackarr@$USER.service
```

### Docker

Docker images can be found on the right under the "Packages" section.

See `docker-compose.yml` for an example.

Make sure you use the correct path you have mapped within the container in the config file. After the first start you
will need to adjust the created config file to your needs and start the container again.

## Configuration

You can configure a decent part of the features seasonpackarr provides. I will explain the most important ones here in
more detail.

### Config Reload

seasonpackarr watches the active `config.yaml` file. It loads each change into a new snapshot, reapplies
`SEASONPACKARR__...` environment overrides, and validates the complete result before it becomes active. An invalid or
partially written file is rejected. The last valid config stays active.

These settings reload while seasonpackarr runs:

- torrent client connections and import policies
- API token
- smart mode and fuzzy matching
- Discord webhook and notification levels
- log level

These settings configure components that start once and require a restart:

- server host and port
- log path, maximum size, and backup count
- `disableConfigFile`

The restart after the first generated config edit is still required because the initial process exits when its client
configuration is incomplete.

### Torrent Client Configuration

Each entry under `clients` connects seasonpackarr to one torrent client instance. Set the `type` field to
`"qbittorrent"` (default), `"transmission"`, `"deluge-v1"`, or `"deluge-v2"` to select the client type. `"deluge"`
is an alias for `"deluge-v2"`. Each client also carries an `import` block that controls how season packs are imported
back into it, see [Import Policy](#import-policy).

#### qBittorrent

For qBittorrent clients, you can authenticate with the traditional `username` and `password` fields, or with `apiKey`
when using qBittorrent 5.2.0 or newer. If `apiKey` is set, seasonpackarr uses qBittorrent API key authentication for
that client instead of username/password login.

If you use [qui's reverse proxy](https://getqui.com/docs/features/reverse-proxy/), set the client `host` to the full
proxy URL, for example `http://localhost:7476/proxy/abc123...`, and leave `username`, `password`, and `apiKey` empty.
qui keeps the qBittorrent session and handles authentication for proxied clients.

#### Transmission

For Transmission clients, set `type: "transmission"` and provide `username` and `password` for the Transmission RPC
interface (no `apiKey` field). Transmission listens on port `9091` by default, so set `port: 9091` (seasonpackarr does
not assume a port if it is left unset).

#### Deluge

Deluge support uses the native daemon RPC interface through
[`autobrr/go-deluge`](https://github.com/autobrr/go-deluge). Set `type: "deluge-v1"` for Deluge 1.3 or
`type: "deluge-v2"` for Deluge 2. The two protocol generations have different wire formats, so the type must match
the daemon. `"deluge"` remains a V2 alias. Use a daemon account from Deluge's `auth` file as `username` and `password`.
The default native RPC port is `58846` when `port` is left unset. Enter a hostname or IP address in `host`, not an HTTP
URL, and do not use the Deluge Web port.

Deluge requires `import.savePath`. It accepts zero or one `import.tags` entry as the torrent's Label plugin label.
Enable Deluge's optional Label plugin to apply it. When enabled, seasonpackarr creates a missing label and assigns it.
Deluge does not support `apiKey` or the qBittorrent-only import fields. The adapter adds the torrent stopped, applies
its optional label, then resumes it. Deluge and libtorrent perform the initial data check before transferring pieces.
Seasonpackarr waits until the torrent is no longer paused or checking. The current adapter requires a v1 or hybrid
torrent because it identifies the torrent by its legacy info hash. This is an adapter limitation, not a general claim
about current Deluge releases.

The [Deluge primary-source audit](docs/references/deluge-primary-source-audit.md) records the exact Autobrr,
go-deluge, Deluge 1.3.15, and Deluge 2 revisions that support these protocol, label, path, duplicate, and checking
decisions.

#### Import Policy

The per-client `import` block controls how seasonpackarr imports a matched season pack back into the client:

- **savePath**: The final import destination. Matched episodes are hardlinked into the season pack folder beneath it,
  and the torrent is added to the client with this save path. It is required for Deluge. It is optional for
  qBittorrent, which follows its automatic-management and manual category-path preferences, and Transmission, which
  falls back to its session download directory. When set, the directory must already exist.
- **tags**: qBittorrent tags or Transmission labels added to imported torrents. Deluge accepts at most one entry as
  its optional Label plugin label. Labels use letters, digits, underscores, or hyphens and are stored in lower case.
  Defaults to `["seasonpackarr"]`.

seasonpackarr adds the torrent in a stopped state. qBittorrent and Transmission complete their explicit verification
before resume. Deluge resumes into its normal initial check. A correctly imported pack is never left stopped.

The following fields are qBittorrent-only and are rejected at startup when set on a Transmission or Deluge client:

- **category**: The category to add the torrent with; also resolves the import destination when `savePath` is empty.
  When no save or download path is set, seasonpackarr sends only the category and leaves path selection and Auto TMM
  to qBittorrent. seasonpackarr reads the same qBittorrent preferences before it creates hardlinks, so both programs
  use the same destination.
- **downloadPath**: A temporary path for incomplete downloads only; never the final destination.
- **contentLayout**: One of `"subfolder"`, `"nosubfolder"` or `"original"`; leave it empty to defer to qBittorrent's
  default.

A qBittorrent client must set either `import.savePath` or `import.category`. Transmission has no categories and no
content layout, so configure `import.savePath` or leave it empty to use the session download directory. Deluge requires
`import.savePath`. Transmission forces a hash check after add. Deluge uses its normal initial check before transferring
pieces. Both clients account for present data, so only genuinely missing episodes get downloaded.

#### Use multiple Sonarr instances with one qBittorrent instance

If multiple Sonarr instances share one qBittorrent instance, add that qBittorrent connection to seasonpackarr once for
each Sonarr instance. Give each entry a unique name and the qBittorrent category used by that Sonarr instance:

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

In qBittorrent, configure each category to save to the folder used by its Sonarr instance. Then create one autobrr
filter for each seasonpackarr entry. Use `"clientname": "sonarr-hd"` in every seasonpackarr payload for the HD filter
and `"clientname": "sonarr-uhd"` for the UHD filter. See [autobrr Filter setup](#autobrr-filter-setup) for the complete
filter and webhook configuration.

Both entries use the same qBittorrent torrent list. qBittorrent can store a torrent only once, so the same torrent
cannot be added separately to both categories.

### Smart Mode

Enable smart mode by setting `smartMode` to `true`. `smartModeThreshold` defines the minimum reusable share of the
announced torrent. seasonpackarr counts distinct valid episode files in the torrent and compares them with exact
target files that it can reuse from the client. MKV and MP4 episodes are supported. Samples, extra videos, and
container mismatches do not count. It does not use an external episode-count provider.

For example, if the torrent contains 12 episode files and the client can reuse 8, the coverage is `8/12 = 0.67`.
A threshold of `0.75` rejects that pack. A client can contain more episodes than the pack, but duplicate or unrelated
episodes cannot increase coverage above 100 percent.

### Parse Torrent

seasonpackarr always parses the torrent file of an announced season pack. This makes sure that the season pack folder
that gets created by seasonpackarr will always have the correct name. One example that will make the benefit of this
clearer:

- Announce name: `Show.S01.1080p.WEB-DL.DDPA5.1.H.264-RlsGrp`
- Folder name: `Show.S01.1080p.WEB-DL.DDP5.1.H.264-RlsGrp`
   
Using the announce name would create the wrong folder and would lead to all the files in the torrent being downloaded
again. The issue in the given example is the additional `A` after `DDP` which is not present in the folder name. By
using the parsed folder name the files will be hardlinked into the exact folder that is being used in the torrent.

Parsing first happens on `POST /api/pack`. That endpoint builds and caches an exact import plan without changing the
filesystem or torrent client. `POST /api/parse` reuses the accepted plan, hardlinks the matched episodes, and imports
the season pack. If the short-lived plan is unavailable after a restart or delay, `/api/parse` safely rebuilds it.

### Fuzzy Matching

In this section, you can toggle comparing rules. I will explain each of them in more detail here.

1. **skipRepackCompare**: When set to `true`, the comparer skips checking the repack status of the season pack release
   against the episodes in your client. The episode in the example will only be accepted as a match by seasonpackarr if
   you enable this option:
   - Announce name: `Show.S01.1080p.WEB-DL.DDPA5.1.H.264-RlsGrp`
   - Episode name: `Show.S01E01.1080p.WEB-DL.REPACK.DDPA5.1.H.264-RlsGrp`

2. **simplifyHdrCompare**: If set to `true`, this option simplifies the HDR formats `HDR10`, `HDR10+`, and `HDR+` to
   just `HDR`. This increases the likelihood of matching renamed releases that specify a more advanced HDR format in the
   announce name than in the episode title:
   - Announce name: `Show.S01.2160p.WEB-DL.DDPA5.1.DV.HDR10+.H.265-RlsGrp`
   - Episode name: `Show.S01E01.2160p.WEB-DL.DDPA5.1.DV.HDR.H.265-RlsGrp`

3. **simplifyWebCompare**: If set to `true`, this option treats `WEB-DL` and `WEB` as the same source.

4. **skipYearCompare**: If set to `true`, this option allows a release without a year to match one that includes a year.

### Recommended options

Keep in mind, these settings are suggestions based on my own use case so feel free to adjust them according to your
specific needs.

```yaml
smartMode: true
smartModeThreshold: 0.75
fuzzyMatching:
  skipRepackCompare: true
  simplifyHdrCompare: false
```

These will filter out most unwanted season packs and prevent mismatches, while still making sure that
renamed season packs and episodes can get matched.

## autobrr Filter setup

Configure one dedicated autobrr filter for each seasonpackarr client and import destination. The following example
allows 1080p season packs and uses the current webhook flow.

### Create Filter

To import it into autobrr you need to navigate to `Filters` and click on the arrow next to `+ Create Filter` to see the
option `Import filter`. Just paste the content below into the text box that appeared and click on `Import`.

```json
{
  "name": "arr-Seasonpackarr",
  "version": "1.0",
  "data": {
    "enabled": true,
    "seasons": "1-99",
    "episodes": "0",
    "resolutions": [
      "1080p",
      "1080i"
    ]
  }
}
```

In the `General` tab you will need to adjust the value of `Priority` to be set higher than all your TV show filters. For
instance, if your Sonarr filter is set at `10` and a TV filter that sends to qBittorrent is at `15`, then you should set
the `seasonpackarr` filter to at least `16`. This ensures that it will execute before the others. It's perfectly fine to
have a `cross-seed` filter positioned above the `seasonpackarr` filter.

### External Filters

Add two ordered Webhook entries in the `External` tab. Both use `POST` and expect HTTP status `200`.
autobrr shows the up and down reorder arrows only after the filter has multiple external entries.

The first entry is a cheap announce-only candidate check. Its endpoint is:

```
http://host:port/api/candidate
```

Its `Data (JSON)` is:

```json
{
  "name": "{{ .TorrentName }}",
  "clientname": "default"
}
```

The second entry is the exact torrent-aware check. Its endpoint is:

```
http://host:port/api/pack
```

Its `Data (JSON)` includes the torrent bytes:

```json
{
  "name": "{{ .TorrentName }}",
  "torrent": "{{ .TorrentDataRawBytes | js }}",
  "clientname": "default"
}
```

Replace the `clientname` value, in this case `default`, with the name you gave your desired torrent client in your
config under the `clients` section. Use the same value in both entries. If you omit `clientname`, seasonpackarr tries
the `default` client. The request fails if that client does not exist.

autobrr runs the entries in order. A rejected announce stops at `/api/candidate`, so autobrr does not download its
torrent file. A candidate that passes continues to `/api/pack`, where seasonpackarr checks the actual torrent and
builds the import plan. Both endpoints are match gates. They do not change the filesystem or torrent client.

After you save the filter, reload it and confirm that `/api/candidate` is displayed above `/api/pack`. The persisted
top-to-bottom display order is the execution order. If necessary, use the arrows beside the entries to correct it,
then save and reload again.

#### API Authentication

I strongly suggest enabling API authentication by providing an API token in the config. The following command will
generate a token for you that you can copy and paste into your config:

```bash
seasonpackarr gen-token
```

After you set the API token, include it on both external webhook entries and the `/api/parse` action. Use the
`X-API-Token` request header for the two external entries. The autobrr Webhook action has no request-header field, so
put the token in the `/api/parse` endpoint as a query parameter.

1. **External entries**: Edit `HTTP Request Headers` on both entries and replace `api_token` with the token from your
   config.
    ```
    X-API-Token=api_token
    ```
2. **Parse action**: Append `?apikey=api_token` to its endpoint.
    ```
    http://host:port/api/parse?apikey=api_token
    ```

The external filter you just created will be disabled by default. To avoid unwanted downloads, make sure to enable it!

### Actions

The only action your filter needs is the Webhook action described below. When it hits `/api/parse`, seasonpackarr
parses the torrent file, hardlinks the matching episodes into the correct season pack folder, adds the torrent to your
torrent client, checks it so the already present files are recognised, and starts it. Do **not** add a separate
torrent-client action to the filter. seasonpackarr owns adding the torrent to the client, and another client action
would add it a second time.

#### Webhook

Navigate to the `Actions` tab, click on `Add new` and change the `Action type` of the newly added action to `Webhook`.
The `Endpoint` field should look like this, with `host`, `port` and `api_token` taken from your config:

```
http://host:port/api/parse?apikey=api_token
```

Append the API query parameter `?apikey=api_token` only if you have enabled API authentication by providing an API token
in your config.

Finally, complete the `Payload (JSON)` field as shown below. Ensure that the value of `clientname` is the same as in the `External Filter`:

```json
{
  "name":"{{ .TorrentName }}", 
  "torrent":"{{ .TorrentDataRawBytes | js }}",
  "clientname": "default"
}
```

Where the season pack ends up and how it is added - save path, category, tags, and content layout - is controlled by the
per-client [Import Policy](#import-policy) in your seasonpackarr config, not by autobrr.

## Credits

Huge credit goes to [upgraderr](https://github.com/KyleSanderson/upgraderr) and specifically [@KyleSanderson](https://github.com/KyleSanderson), whose
project provided great functions that I could make use of. Additionally, I would also like to mention [@zze0s](https://github.com/zze0s), who was
really helpful regarding any question I had as well as providing me with a lot of the structure this project has now.
Last but not least, a big thank you to [JetBrains](http://www.jetbrains.com/)
for providing me with free licenses to their great tools, in this case [GoLand](https://www.jetbrains.com/go/).
