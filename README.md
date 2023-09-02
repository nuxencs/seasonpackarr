# seasonpackarr

A companion app for autobrr that will automatically hardlink downloaded episodes into a season pack folder as soon as the season pack gets announced. This way you won't have to download any episodes that you already have.

Huge credit goes to [upgraderr](https://github.com/KyleSanderson/upgraderr) and specifically [@KyleSanderson](https://github.com/KyleSanderson), whose project provided great functions that I could make use of.

> **Warning**
> This application is still in the very early stages of development, so expect bugs to happen, especially with weird episode or season pack naming.

## Installation

### Linux

Download the latest release, or download the [source code](https://github.com/nuxencs/seasonpackarr/releases/latest) and build it yourself using `go build`.

```bash
wget $(curl -s https://api.github.com/repos/nuxencs/seasonpackarr/releases/latest | grep download | grep linux_x86_64 | cut -d\" -f4)
```

#### Unpack

Run with `root` or `sudo`. If you do not have root, or are on a shared system, place the binaries somewhere in your home directory like `~/.bin`.

```bash
tar -C /usr/bin -xzf seasonpackarr*.tar.gz
```

This will extract `seasonpackarr` to `/usr/bin`.
Note: If the command fails, prefix it with `sudo ` and re-run again.

#### Systemd (Recommended)

On Linux-based systems, it is recommended to run seasonpackarr as a sort of service with auto-restarting capabilities, in order to account for potential downtime. The most common way is to do it via systemd.

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
ExecStart=/usr/bin/seasonpackarr --config=/home/%i/.config/seasonpackarr/config.toml

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

On first run it will create a default config, `~/.config/seasonpackarr/config.toml` that you will need to edit.

After the config is edited you need to restart the service `sudo systemctl restart seasonpackarr@$USER.service`.

### Docker

You find the docker image on the right side under "Packages" 

See `docker-compose.yml` for an example.

Make sure you use the correct path you have mapped within the container in the config file. After the first start you will need to set up the created config file in your config directory and start the container again.

## autobrr Filter setup

You can import this filter into your autobrr instance. Currently, seasonpackarr only supports one output folder, so if you have multiple Sonarr instances with different pre import directories, you need to create multiple filters and run multiple instances of seasonpackarr. The filter below is an example for a 1080p instance.

```
{
    "name": "arr-Seasonpackarr",
    "version": "1.0",
    "data": {
        "enabled": true,
        "priority": 15,
        "seasons": "1-99",
        "episodes": "0",
        "resolutions": [
            "1080p",
            "1080i"
        ]
    }
}
```

After adding this filter, you need to go to the `External` tab and enable the `Webhook` functionality. `Host` should look like this, with host and port from the config: `http://<host>:<port>/api/pack`. `Expected HTTP status` has to be set to `250`. Finally `Data (JSON)` needs to look like this, with the variables replaced by your information:

```
{ "host":"http://<qbit_host>:<qbit_port>",
  "user":"<qbit_user>",
  "password":"<qbit_pass>",
  "name":"{{ .TorrentName | js }}" }
```

Next you need to go to the `Actions` tab, click on `Add new` and select `qBittorrent` in the `Type` and your configured qBittorrent client in the `Client` field. If you use seasonpackarr together with Sonarr you can input the pre import category in the `Category` field to make sure it picks up the season pack.

If you want to extract the correct folder name from the .torrent file and not from the announce name to make sure that it's always correct, you will need to set the config option `parseTorrentFile` to `true`. Additionally, you need to create a new action, select `Webhook` in the `Type` field, with `Host` looking like this, replacing the host and port with the ones from your config: `http://<host>:<port>/api/push`. Finally `Data (JSON)` needs to be filled with this, where the variables need to be replaced by your information:

```
{ "host":"http://<qbit_host>:<qbit_port>",
  "user":"<qbit_user>",
  "password":"<qbit_pass>",
  "name":"{{ .TorrentName | js }}" 
  "torrentpath":"{{ .TorrentPathName | js }}" }
```

You need to make sure that the `Webhook` action is the first one in your list followed by the `qBittorrent` action.

Last but not least, you should leave `Skip Hash Check` disabled to prevent any torrents added by seasonpackarr from erroring in your qBittorrent client when you are missing some episodes of a season.

> **Warning**
> If you enable that option regardless, you will most likely have to deal with errored torrents, which would require you to manually trigger a recheck on them to fix the issue.
