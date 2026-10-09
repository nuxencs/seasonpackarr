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
5. Transmission entries. [pending, #271]
6. Deluge build definition and entries. [pending, #272]
7. CI workflow. [pending, #273]
8. Seeder and qBittorrent real download test. [pending, #274]
9. Transmission and Deluge real download tests. [pending]
10. Move this plan to `completed/`. [pending]

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
