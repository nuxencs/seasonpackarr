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
default_entries=(qbit-4.3.9 qbit-5.2.4)
# Middle versions that only `all` runs. hotio publishes no 4.4.x image.
extra_entries=(qbit-4.5.5 qbit-4.6.7 qbit-5.0.5 qbit-5.1.4)

harness_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$harness_dir/../../../.." && pwd)
artifacts_dir="$harness_dir/artifacts"
# One project per checkout, so two worktrees can run at the same time.
project="seasonpackarr-harness-$(printf '%s' "$repo_root" | cksum | cut -d ' ' -f 1)"

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

# configure_entry sets client, test_pattern, and the image variables that the
# client's compose overlay reads.
configure_entry() {
	case $1 in
	qbit-*)
		client=qbit
		test_pattern=QbitDaemon_
		QBIT_IMAGE="ghcr.io/hotio/qbittorrent:release-${1#qbit-}"
		if [[ $1 == qbit-4.3.9 ]]; then
			# hotio publishes 4.3.9 only under the moving legacy tag.
			QBIT_IMAGE=ghcr.io/hotio/qbittorrent:legacy@sha256:d32eb9f62a4f70878a2268cb479b2a4034a18b9ee42e26193909fedde5b1ad96
		fi
		export QBIT_IMAGE
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
	local dir="$artifacts_dir/$1" service
	mkdir -p "$dir"
	for service in $(compose ps --all --services); do
		[[ $service == test ]] && continue
		compose logs --no-color --timestamps "$service" >"$dir/$service.log" 2>&1 || true
	done
	case $client in
	qbit)
		# qBittorrent writes its event log to the config volume, not to stdout.
		compose cp qbit:/config/data/logs/qbittorrent.log "$dir/qbittorrent.log" >/dev/null 2>&1 || true
		;;
	esac
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
