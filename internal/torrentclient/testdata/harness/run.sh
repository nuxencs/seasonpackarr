#!/usr/bin/env bash
# Runs the torrent client integration tests against real daemons in Docker Compose.
#
# Usage:
#   run.sh               run the default entries (the CI set)
#   run.sh all           run the default entries and the middle qBittorrent versions
#   run.sh <entry>...    run the named entries, for example qbit-4.3.9
#   run.sh list [all]    print the entries as a JSON array
#
# Each entry starts fresh daemons and a fresh /data volume, runs that client's
# integration tests in strict mode, saves the daemon logs to artifacts/<entry>
# when it fails, and removes its containers and volumes.
set -euo pipefail

# Oldest and newest supported version of each client. Bump the newest by hand.
default_entries=(
	qbit-4.3.9 qbit-5.2.4
	transmission-4.0.6 transmission-4.1.3
	deluge-1.3.15 deluge-2.0.3 deluge-2.2.0
)
# Middle versions that only `all` runs. hotio publishes no 4.4.x image.
extra_entries=(qbit-4.5.5 qbit-4.6.7 qbit-5.0.5 qbit-5.1.4)

harness_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$harness_dir/../../../.." && pwd)
artifacts_dir="$harness_dir/artifacts"
# One project per checkout, so two worktrees can run at the same time.
project="seasonpackarr-harness-$(printf '%s' "$repo_root" | cksum | cut -d ' ' -f 1)"

# usage prints the Usage lines of the header comment, lines 4 to 8 of this file.
usage() {
	sed -n '4,8s/^# \{0,1\}//p' "${BASH_SOURCE[0]}" >&2
	exit 2
}

json_array() {
	local separator=''
	printf '['
	for item in "$@"; do
		printf '%s"%s"' "$separator" "$item"
		separator=','
	done
	printf ']\n'
}

is_known_entry() {
	local known
	for known in "${default_entries[@]}" "${extra_entries[@]}"; do
		[[ $known == "$1" ]] && return 0
	done
	return 1
}

# configure_entry sets client, test_pattern, log_files, and the image variables
# that the client's compose overlay reads. log_files lists <service>:<path>
# entries for daemon logs that are not on stdout.
configure_entry() {
	case $1 in
	qbit-*)
		client=qbit
		test_pattern=QbitDaemon_
		log_files=(qbit:/config/data/logs/qbittorrent.log)
		QBIT_IMAGE="ghcr.io/hotio/qbittorrent:release-${1#qbit-}"
		if [[ $1 == qbit-4.3.9 ]]; then
			# hotio publishes 4.3.9 only under the moving legacy tag.
			QBIT_IMAGE=ghcr.io/hotio/qbittorrent:legacy@sha256:d32eb9f62a4f70878a2268cb479b2a4034a18b9ee42e26193909fedde5b1ad96
		fi
		export QBIT_IMAGE
		;;
	transmission-*)
		client=transmission
		test_pattern=TransmissionDaemon_
		# transmission-daemon -f logs to the container output, which save_logs captures.
		log_files=()
		# Plain version tags move with every weekly rebuild. The build tags do not.
		case $1 in
		transmission-4.0.6) TRANSMISSION_IMAGE=lscr.io/linuxserver/transmission:4.0.6-r4-ls311 ;;
		transmission-4.1.3) TRANSMISSION_IMAGE=lscr.io/linuxserver/transmission:4.1.3-r0-ls363 ;;
		*)
			printf 'no Transmission image for %s\n' "$1" >&2
			exit 2
			;;
		esac
		export TRANSMISSION_IMAGE
		;;
	deluge-*)
		client=deluge
		test_pattern=DelugeDaemon_
		# deluged --do-not-daemonize logs to the container output.
		log_files=()
		# deluge/Dockerfile installs the exact Debian package version.
		case $1 in
		deluge-1.3.15) DELUGE_DEBIAN_RELEASE=buster DELUGE_VERSION=1.3.15-2 DELUGE_CLIENT_TYPE=deluge-v1 ;;
		deluge-2.0.3) DELUGE_DEBIAN_RELEASE=bookworm DELUGE_VERSION=2.0.3-4 DELUGE_CLIENT_TYPE=deluge-v2 ;;
		deluge-2.2.0) DELUGE_DEBIAN_RELEASE=trixie DELUGE_VERSION=2.2.0-1 DELUGE_CLIENT_TYPE=deluge-v2 ;;
		*)
			printf 'no Deluge package for %s\n' "$1" >&2
			exit 2
			;;
		esac
		export DELUGE_DEBIAN_RELEASE DELUGE_VERSION DELUGE_CLIENT_TYPE
		;;
	esac
}

compose() {
	docker compose --project-directory "$harness_dir" --project-name "$project" \
		--file "$harness_dir/compose.yaml" --file "$harness_dir/compose.$client.yaml" "$@"
}

teardown() {
	[[ -n ${client:-} ]] || return 0
	local output
	if ! output=$(compose down --volumes --remove-orphans --timeout 30 2>&1); then
		printf 'teardown of %s failed:\n%s\n' "$project" "$output" >&2
	fi
}

save_logs() {
	local dir="$artifacts_dir/$1" service log_file
	mkdir -p "$dir"
	for service in $(compose ps --all --services); do
		[[ $service == test ]] && continue
		compose logs --no-color --timestamps "$service" >"$dir/$service.log" 2>&1 || true
	done
	# The + form expands an empty array without an unbound error in bash 3.2.
	for log_file in ${log_files[@]+"${log_files[@]}"}; do
		compose cp "$log_file" "$dir/$(basename "$log_file")" >/dev/null 2>&1 || true
	done
	printf 'saved daemon logs to %s\n' "$dir" >&2
}

run_entry() {
	local entry=$1 status
	configure_entry "$entry"
	printf '\n==> %s\n' "$entry"
	rm -rf "${artifacts_dir:?}/$entry"
	# A killed earlier run can leave containers and volumes behind.
	teardown
	if compose pull --quiet --ignore-buildable &&
		compose build &&
		compose run --rm test go test -tags=integration -count=1 -v -run "$test_pattern" ./internal/torrentclient; then
		status=0
	else
		status=$?
		save_logs "$entry"
	fi
	teardown
	return "$status"
}

case ${1:-} in
list)
	[[ $# -le 2 ]] || usage
	case ${2:-} in
	'') json_array "${default_entries[@]}" ;;
	all) json_array "${default_entries[@]}" "${extra_entries[@]}" ;;
	*) usage ;;
	esac
	exit 0
	;;
-h | --help) usage ;;
'') entries=("${default_entries[@]}") ;;
all)
	[[ $# -eq 1 ]] || usage
	entries=("${default_entries[@]}" "${extra_entries[@]}")
	;;
*) entries=("$@") ;;
esac

for entry in "${entries[@]}"; do
	if ! is_known_entry "$entry"; then
		printf 'unknown entry %s, known entries: %s %s\n' "$entry" "${default_entries[*]}" "${extra_entries[*]}" >&2
		exit 2
	fi
done

GO_IMAGE=$(awk '$1 == "FROM" && $2 ~ /^golang:/ { print $2; exit }' "$repo_root/Dockerfile")
[[ -n $GO_IMAGE ]] || {
	echo "no golang image in $repo_root/Dockerfile" >&2
	exit 1
}
export GO_IMAGE
docker volume create seasonpackarr-harness-go-cache >/dev/null

trap teardown EXIT
trap 'exit 130' INT TERM

failed=()
for entry in "${entries[@]}"; do
	run_entry "$entry" || failed+=("$entry")
done

printf '\nentries: %s\n' "${entries[*]}"
if [[ ${#failed[@]} -gt 0 ]]; then
	printf 'failed: %s\n' "${failed[*]}" >&2
	exit 1
fi
echo 'all entries passed'
