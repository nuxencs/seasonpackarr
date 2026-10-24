#!/bin/sh
# Starts deluged for the integration harness with the committed test account.
set -eu

config_dir=/config
mkdir -p "$config_dir"
# Auth level 10 is admin.
printf '%s\n' 'seasonpackarr:integration:10' >"$config_dir/auth"
chmod 600 "$config_dir/auth"
# Deluge 1.3 and 2 both read this version header and config object, and fill
# the missing keys with their defaults. Without the header, each save logs a
# warning. The tests need no peer discovery, port mapping, or internet access.
cat >"$config_dir/core.conf" <<'EOF'
{
  "file": 1,
  "format": 1
}{
  "dht": false,
  "lsd": false,
  "utpex": false,
  "upnp": false,
  "natpmp": false,
  "new_release_check": false
}
EOF

# The default RPC bind is loopback only, which the test container cannot reach.
exec deluged --do-not-daemonize --config "$config_dir" --ui-interface 0.0.0.0 --port 58846 --loglevel info
