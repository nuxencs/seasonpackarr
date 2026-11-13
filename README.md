<h1 align="center">seasonpackarr</h1>
<p align="center">
  <a href="https://github.com/nuxencs/seasonpackarr/blob/develop/LICENSE"><img src="https://img.shields.io/github/license/nuxencs/seasonpackarr?style=flat-square&color=00ACD7" alt="License"></a>
  <a href="https://github.com/nuxencs/seasonpackarr/actions"><img src="https://img.shields.io/github/actions/workflow/status/nuxencs/seasonpackarr/release.yml?style=flat-square&logo=github" alt="Build"></a>
  <a href="https://github.com/nuxencs/seasonpackarr/releases"><img src="https://img.shields.io/github/v/release/nuxencs/seasonpackarr?style=flat-square&color=00ACD7" alt="Latest Release"></a>
  <a href="https://trash-guides.info/discord"><img src="https://img.shields.io/discord/492590071455940612?style=flat-square&logo=discord&logoColor=00ACD7&label=support&color=00ACD7" alt="Discord"></a>
</p>

<p align="center">
Get TV season packs without downloading the episodes you already have.
</p>

seasonpackarr finds season packs through [autobrr](https://github.com/autobrr/autobrr) announces or Prowlarr. It
**hardlinks** the episodes that your torrent client already has into the season pack folder, then adds the pack to the
client. The client downloads only the missing episodes.

> [!IMPORTANT]
> Upgrading from v0.16.0? The configuration and the autobrr filter changed. Follow the
> [upgrade guide](docs/UPGRADE-v1.md) before you replace the service.

## How it works

1. **Candidate check**: seasonpackarr compares the release name with the episodes in your torrent client. A release
   that cannot be useful stops here, before any torrent download.
2. **Match**: seasonpackarr reads the torrent and finds the episode files on disk that it can reuse. This step changes
   nothing.
3. **Import**: seasonpackarr hardlinks the reusable episodes into the exact folder of the torrent and adds the pack to
   your client. The client checks the present files and downloads the rest.

## Documentation

- [Installation](docs/INSTALL.md): run the binary, systemd, or Docker, and complete the first configuration.
- [Configuration](docs/CONFIGURATION.md): torrent clients, import policy, smart mode, fuzzy matching, and config reload.
- [autobrr setup](docs/AUTOBRR.md): the filter, the External checks, and the import action.
- [Prowlarr discovery](docs/PROWLARR.md): RSS monitoring and search for season packs.
- [Command-line guide](docs/CLI.md): check a release or a torrent from the terminal.
- [API reference](docs/API.md): endpoints, authentication, and status codes.
- [Upgrade from v0.16.0](docs/UPGRADE-v1.md): the required changes for v1.0.0.
- [Development](docs/DEVELOPMENT.md) and [architecture](ARCHITECTURE.md): build, test, and contribute.

## Features

- **Torrent clients**: qBittorrent 4.3.9 to 5.2.4 (including API keys and the qui reverse proxy), Transmission 4.0.6 to
  4.1.3, and Deluge 1.3.15 and 2.0.3 to 2.2.0.
- **Two sources**: autobrr announces, Prowlarr RSS monitoring, and targeted Prowlarr searches. Use one or combine them.
- **Exact folder names**: the pack folder name comes from the torrent, not the announce, so a renamed release does not
  download again.
- **Smart mode**: skip a season pack when too few of its episodes are already on disk.
- **Fuzzy matching**: optional rules for repacks, HDR formats, WEB sources, and years.
- **Import policy for each client**: save path, category, tags, and content layout.
- **Live config reload**: most settings apply without a restart, and an invalid edit keeps the last valid config.
- **Discord notifications** with configurable levels.
- **No database setup**: discovery state stays in an SQLite file beside the config.

## Support

Ask questions in the #seasonpackarr channel on the TRaSH-Guides [Discord server](https://trash-guides.info/discord).
Report bugs and request features in [GitHub issues](https://github.com/nuxencs/seasonpackarr/issues). To report a
security problem, see [SECURITY.md](SECURITY.md).

## Credits

Huge credit goes to [upgraderr](https://github.com/KyleSanderson/upgraderr) and specifically [@KyleSanderson](https://github.com/KyleSanderson), whose
project provided great functions that I could make use of. Additionally, I would also like to mention [@zze0s](https://github.com/zze0s), who was
really helpful regarding any question I had as well as providing me with a lot of the structure this project has now.
