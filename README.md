# seasonpackarr

A companion app for autobrr that will automatically hardlink downloaded episodes into a season pack folder as soon\
as the season pack gets announced. This way you won't have to download any episodes that you already have.

Huge credit goes to [upgraderr](https://github.com/KyleSanderson/upgraderr) and specifically [@KyleSanderson](https://github.com/KyleSanderson), whose project provided great\
functions that I could make use of.

## Installation

### Linux

Download the latest release, or download the [source code](https://github.com/nuxencs/seasonpackarr/releases/latest) and build
it yourself using `go build`.

```bash
wget $(curl -s https://api.github.com/repos/nuxencs/seasonpackarr/releases/latest | grep download | grep linux_x86_64 | cut -d\" -f4)
```

#### Unpack

Run with `root` or `sudo`. If you do not have root, or are on a shared system, place the binaries somewhere in your home
directory like `~/.bin`.

```bash
tar -C /usr/bin -xzf seasonpackarr*.tar.gz
```

This will extract `seasonpackarr` to `/usr/local/bin`.
Note: If the command fails, prefix it with `sudo ` and re-run again.

#### Systemd (Recommended)

On Linux-based systems, it is recommended to run seasonpackarr as a sort of service with auto-restarting capabilities, in
order to account for potential downtime. The most common way is to do it via systemd.

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

After the config is edited you need to restart the service `sudo systemctl restart omegabrr@$USERNAME.service`.
