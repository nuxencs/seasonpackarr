#!/bin/sh
set -eu

config_dir=/config
mkdir -p "$config_dir"
printf '%s\n' 'seasonpackarr:integration:10' > "$config_dir/auth"
chmod 600 "$config_dir/auth"

exec deluged --do-not-daemonize --config "$config_dir" --ui-interface 0.0.0.0 --port 58846 --loglevel info
