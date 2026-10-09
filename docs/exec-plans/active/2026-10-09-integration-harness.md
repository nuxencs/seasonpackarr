# Integration test harness

Tracks #269.

## goal

One checked-in command starts pinned torrent client daemons in Docker Compose
and runs the integration suites against them. The same command runs locally
and in CI. A real download test checks the core promise: only the missing
episodes download, and the reused episodes stay hardlinks.

## scope

- runner, compose files, and committed daemon config under
  `internal/torrentclient/testdata/harness/` (#270)
- strict mode: `SEASONPACKARR_TEST_REQUIRE_DAEMONS=1` makes the shared daemon
  gate fail instead of skip (#270)
- qBittorrent entries 4.3.9 to 5.2.4 from hotio images (#270)
- Transmission entries 4.0.6 and 4.1.3 (#271)
- Deluge entries 1.3.15, 2.0.3 and 2.2.0 from one committed build definition (#272)
- CI workflow that reads its matrix from the runner (#273)
- seeder and real download tests for every client (#274 and later)
- README support statement, `ARCHITECTURE.md` "Testing Surface", and the
  `AGENTS.md` Verification section

## non-goals

- production code changes
- the full autobrr to HTTP API to client flow
- automatic image version bumps
- a required CI check

## risks

- An upstream image change under a moving tag changes results. Exact tags,
  and a digest for hotio `legacy`, prevent this.
- qBittorrent 4.3 and 4.5+ read DHT, PEX and LPD from different config keys.
  A wrong key leaves peer discovery on and makes the real download test
  depend on the network.
- Docker Desktop bind mounts on macOS break hardlinks (ADR-0001). Only the
  named `/data` volume may hold packs.
- A missing environment variable skips tests and looks green. Strict mode
  prevents this in the harness.

## steps

1. Runner with `run`, `all`, and `list [all]`, base compose file, and the
   qBittorrent overlay. [done, #270]
2. Committed `qBittorrent.conf` with fixed credentials and DHT, PEX and LPD off
   for 4.3 and 4.5+ key names. [done, #270]
3. Strict mode in `requireDaemon`. [done, #270]
4. Docs: README qBittorrent versions, "Testing Surface", AGENTS.md
   Verification. [done, #270]
5. Transmission entries. [done, #271]
6. Deluge build definition and entries. [done, #272]
7. CI workflow. [done, #273]
8. Seeder and qBittorrent real download test. [done, #274]
9. Transmission real download test. [done, #275]
10. Deluge real download test. [done, #276]
11. Move this plan to `completed/`. [pending]

## decision log

- Harness lives in the torrent client package's `testdata` folder, so the Go
  toolchain ignores it.
- One base compose file plus one overlay for each client. The overlay adds the
  daemon and the `depends_on` of the test service, so a new client does not
  change the base file and no service depends on an inactive profile.
- The runner derives the compose project name from the checkout path, so two
  worktrees can run at the same time.
- The runner reads the Go test image from the release `Dockerfile`, so the
  harness always uses the release Go version.
- Go module and build caches are one external volume that survives teardown.
  The `/data` volume and the daemon config volume are project volumes, and the
  runner removes them after every entry.
- Real 4.3.9 and 5.2.4 daemons generated the committed `qBittorrent.conf`
  through `setPreferences`, and the two results are merged. This avoids
  hand-written key names and password hashes.
- Daemons and the test container run as uid 1000. hotio `release-4.5.5`
  refuses `PUID=0` ("Running as root is not supported"), and the daemon must
  be able to write into the packs that the tests create. A root
  `volume-init` step gives the fresh volumes to uid 1000 first.
- Transmission images are linuxserver build tags (`4.0.6-r4-ls311`,
  `4.1.3-r0-ls363`), not the plain version tags. linuxserver rebuilds every
  version weekly on Alpine edge under the same plain tag, so only a build tag
  keeps a result from changing without a commit.
- Transmission starts from a committed partial `settings.json`. The image
  copies its own defaults only when the file is missing, and Transmission
  fills the missing keys with its built-in defaults. The image defaults turn on
  DHT and PEX and enable the incomplete and watch folders at `/downloads` and
  `/watch`, which the harness does not mount. The credentials still come from
  `USER` and `PASS`.
- Deluge images are built from Debian packages with one `deluge/Dockerfile`.
  The build args are the Debian release and the exact `deluged` package
  version (`1.3.15-2` buster, `2.0.3-4` bookworm, `2.2.0-1` trixie). Only
  buster gets other apt sources, because it moved to `archive.debian.org`.
  The base image follows `debian:<release>-slim`. The exact pin is the package
  version: when a Debian update replaces it on the mirror, the build fails
  and does not silently test another version. Deluge 2.0.3 from bookworm
  passed the suite, so it stays the oldest supported Deluge 2 version.
- The Deluge image runs as uid 1000 with `HOME=/config`, because Deluge 1.3
  extracts plugin eggs below HOME. The entrypoint writes `core.conf` with the
  `{"file": 1, "format": 1}` header. Without it, Deluge 1.3 and 2 log
  `Unable to open config file ... list index out of range` on every save.
- The integration workflow reads its matrix from `run.sh list`, so it holds no
  entry names, image tags, or build args. Entry-specific settings stay in the
  runner and the compose overlays.
- The Deluge layer cache is in `compose.deluge.yaml`, behind
  `HARNESS_GHA_CACHE`, and not in a separate `build-push-action` step. A
  separate step would copy the build args of each Deluge entry into the
  workflow. The cache needs three settings in the job:
  - a `docker-container` builder from `setup-buildx-action`
  - `COMPOSE_BAKE=true`, so that compose builds on that builder
  - the `ACTIONS_*` variables from `crazy-max/ghaction-github-runtime`, because
    run steps do not get them
- The cache export has `ignore-error=true`, so a cache service error does not
  fail the tests.
- The workflow also runs when `Dockerfile` changes, because the runner reads
  the Go test image from it. It also runs when `internal/domain`,
  `internal/errtrace`, or `internal/torrents` change, because the torrent
  client tests import them.

- The seeder is in the base `compose.yaml`, so every entry starts it and the
  test service waits for its health check. The `qbit` overlay `extends` the
  seeder service and replaces only the image and the config volume, so one
  definition starts both qBittorrent daemons.
- The download test hardlinks the reused episodes from a source folder below
  the import folder, not from the seed folder. The seeder and the client under
  test then share no inodes, and the inode check compares against files that
  only the test writes.
- The download test also checks that the client downloaded exactly one
  episode. A client that rewrites a hardlinked file in place keeps its inode
  and content, and only the byte count shows the redownload. libtorrent adds
  payload to the count once per second, so the qBittorrent wait includes the
  count, not only the progress.
  The Transmission and Deluge tests must give `assertDownloadedPack` the
  payload byte count of their client, and their wait must include it too.
- The test repeats `addPeers` every 2 seconds until the download completes,
  so one failed connect attempt cannot stall it.
- The Transmission test reads the listen port from the session `peer-port`
  and the payload byte count from `downloadedEver`. Its wait also needs status
  `seeding`, because Transmission downloads into `<file>.part` and renames the
  file when it completes.
- A Transmission download starts about 10 seconds after the seeder connects.
  Transmission sets its interest in a peer only in `rechokePulse`, which runs
  every 10 seconds (`RechokePeriod` in `peer-mgr.cc`, 4.0.6 and 4.1). The
  worst case is one period plus the transfer, so the 30-second daemon timeout
  is sufficient.
- The committed Transmission `settings.json` turns off
  `port-forwarding-enabled`, so the daemon does not try UPnP or NAT-PMP.
- The Deluge test reads the listen port from `core.get_listen_port`
  (go-deluge `GetListenPort`). Deluge picks a random listen port by default
  (`random_port`), so the port changes with each daemon start. The payload
  byte count is `all_time_download`, which libtorrent also updates once per
  second, so the wait includes it. The wait also needs `is_finished`.

## verification notes

- #270, 2026-10-09:
  - `run.sh qbit-4.3.9 qbit-5.2.4` passes in strict mode. On 4.3.9 the
    "manual category path" subtest still skips.
  - `run.sh all` passes all six entries, including the middle entries 4.5.5,
    4.6.7, 5.0.5 and 5.1.4.
  - Ctrl-C during an entry removes its containers and volumes.
  - `/api/v2/app/preferences` reports `dht`, `pex` and `lsd` as false on 4.3.9
    and 5.2.4 with the committed config.
  - A forced failure (wrong password) exits 1, writes `qbit.log` and
    `qbittorrent.log` to `artifacts/qbit-5.2.4/`, and leaves no containers or
    project volumes.
  - Strict mode without daemon settings fails the test and names
    `SEASONPACKARR_TEST_QBIT_HOST` and `SEASONPACKARR_TEST_IMPORT_DIR`. Without
    strict mode the test skips.
  - The runner works with macOS `/bin/bash` 3.2.
- #271, 2026-10-09:
  - `run.sh transmission-4.0.6 transmission-4.1.3` passes all three
    Transmission tests in strict mode on both versions.
  - `session-get` reports versions 4.0.6 and 4.1.3, `dht-enabled`,
    `pex-enabled`, `lpd-enabled` and `incomplete-dir-enabled` false. An RPC
    call without credentials gets 401.
  - A forced failure (wrong password) exits 1 with macOS `/bin/bash` 3.2,
    writes `transmission.log` to `artifacts/transmission-4.1.3/`, and leaves
    no containers or project volumes.
  - `run.sh qbit-5.2.4` still passes after the `save_logs` change.
  - Fresh 4.1.3 daemons log `ERR ... Couldn't read '/config/queue.json'`
    once. It is not a failure.
  - On start, 4.1.3 looks up its public IPv4 address (`ip-cache.cc`). The
    real download test needs no internet access. #275 turned off
    `port-forwarding-enabled`.
- #272, 2026-10-09:
  - `run.sh deluge-1.3.15 deluge-2.0.3 deluge-2.2.0` passes both Deluge tests
    in strict mode on all three versions, with no skips. The tests report
    daemon versions 1.3.15, 2.0.3 and 2.2.0.
  - After a clean stop, the `core.conf` that each daemon saved has `dht`,
    `lsd`, `utpex`, `upnp`, `natpmp` and `new_release_check` false and
    `daemon_port` 58846.
  - A forced failure (wrong password) exits 1, writes `deluge.log` to
    `artifacts/deluge-2.2.0/`, and leaves no containers or project volumes.
  - `run.sh list` prints the seven default entries.
  - Deluge 2.0.3 on Python 3.11 logs `Unhandled error in Deferred` with a
    `findCaller` `TypeError` for most log calls. The Deluge audit records it.
    It is not a failure.
- #273, 2026-10-09:
  - `actionlint` passes on `.github/workflows/integration.yml`.
  - `run.sh list` and `run.sh list all` give the 7 and 11 entries that the
    setup job reads.
  - `run.sh deluge-2.2.0` passes on a `docker-container` builder with
    `COMPOSE_BAKE=true` and `HARNESS_GHA_CACHE` unset, so the empty cache
    entries do not break a build.
  - With `HARNESS_GHA_CACHE=1`, `docker compose build --print` gives
    `cache-from` `type=gha,scope=deluge-2.2.0-1` and `cache-to`
    `type=gha,mode=max,ignore-error=true,scope=deluge-2.2.0-1`.
  - On PR #278, the workflow ran the setup job and the 7 default entries as
    parallel jobs, and all passed (53 to 91 seconds each). The runtime step
    exposed `ACTIONS_RESULTS_URL` and `ACTIONS_CACHE_SERVICE_V2`, and the
    first Deluge build exported its cache to gha.
  - A re-run of the `deluge-2.2.0` job imported the gha cache, and the
    `apt-get install` layer was `CACHED`.
  - Not verified: the `full` dispatch, which GitHub offers only after the
    workflow is on `develop`, and a failed job that uploads its logs.
- #274, 2026-10-09:
  - `run.sh qbit-5.2.4 qbit-4.3.9` passes in strict mode, including both
    subtests of `TestQbitDaemon_DownloadsMissingEpisodes` (4 to 8 seconds
    each).
  - Before the seeder service existed, strict mode failed the test and named
    `SEASONPACKARR_TEST_SEEDER_HOST` and `SEASONPACKARR_TEST_SEED_DIR`.
  - `-count=2` against the same daemons passes, so the torrent cleanup on the
    seeder and the client lets reruns pass.
  - A fixture change that copies the reused episodes instead of hardlinking
    them fails the inode check for both reused episodes.
  - A failed entry writes `seeder.log` and `seeder-qbittorrent.log` next to the
    client logs. The qBittorrent log of the client is now
    `qbit-qbittorrent.log`.
  - `run.sh transmission-4.1.3 deluge-2.2.0` still passes with the seeder in
    the base compose file.
  - On PR #278, `transmission-4.0.6` failed once in
    `TestTransmissionDaemon_ResumesPartialPack` with `stopped` at 0.33. A
    rerun passed. In Transmission 4.0.6 the verify thread queues the torrent
    start as a separate session step (`tr_torrentOnVerifyDone`), so a read
    between the check and the start sees `stopped`.
    `waitTransmissionChecked` now waits until the torrent is also started or
    reports an error. `run.sh transmission-4.0.6 transmission-4.1.3` passes.
- #275, 2026-10-09:
  - `run.sh transmission-4.0.6 transmission-4.1.3` passes in strict mode,
    including both subtests of `TestTransmissionDaemon_DownloadsMissingEpisodes`
    (12 to 15 seconds each). The seeder connects to the session `peer-port`
    51413.
  - Polls every 100 ms show that no data arrives for about 10 seconds after
    the seeder connects. Then the transfer takes about 1 second.
    `downloadedEver` is exactly 1048576 bytes.
  - `-count=2` on `transmission-4.0.6` against the same daemons passes, so the
    torrent cleanup lets reruns pass.
  - `session-get` reports `port-forwarding-enabled` false on 4.0.6 and 4.1.3.
- #276, 2026-10-09:
  - `run.sh deluge-1.3.15 deluge-2.0.3 deluge-2.2.0` passes in strict mode,
    including both subtests of `TestDelugeDaemon_DownloadsMissingEpisodes`
    (2 to 5 seconds each). `GetListenPort` reports a random port on each
    daemon start, for example 62158 and 51519.
  - `all_time_download` equals exactly one episode (1048576 bytes) on all
    three versions, because `assertDownloadedPack` checks it.
  - `-count=2` on `deluge-2.2.0` and `deluge-1.3.15` against the same daemons
    passes, so the torrent cleanup lets reruns pass.
  - Deluge 1.3.15 reports `Queued` after the check, before it starts the
    download. The test does not depend on it.
