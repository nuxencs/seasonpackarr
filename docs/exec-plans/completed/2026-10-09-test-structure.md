# Test structure and naming

## goal

Give the test suite one predictable layout so a reader can find, name, and set
up a test without copying a neighbor's quirks. Start with the torrent-client
integration tests, where file names, test names, setup, polling, and cleanup
diverge the most.

## scope

Phase 1, integration tests:

- one integration file per client plus one shared fixture file
- one `Test<Client>Daemon_<Behavior>` subject for integration tests
- shared environment gating, pack builders, import requests, and polling
- teardown through `t.Cleanup` for every torrent the tests add
- testify `require`/`assert` in integration tests, like the rest of the repo
- documented rules in `ARCHITECTURE.md`

Phase 2, every other test:

- test files named after the production file they test, plus
  `fixtures_test.go` for shared fakes and fixtures
- one fake vocabulary: `fake<Thing>` replaces `stub`, `mock`, `recording`,
  `noop`, `static`, and `mutable` doubles
- one captured logger: `internal/logger/loggertest` replaces the copies in
  `config` and `http`
- one table loop variable (`tt`) and table name (`tests`)
- subjects that name the function, command, endpoint, or feature under test
- testify everywhere, and `assert` instead of `require` off the test goroutine
- one import grouping in every Go file

## risks

- Integration tests do not run in CI. A wrong rename or a broken helper shows
  up only when someone runs them against real daemons.
- Removed tests drop coverage if the decision log below is wrong.

## steps

1. Inventory the current integration tests. [done]
2. Move shared fixtures to `fixtures_integration_test.go` and split tests by client. [done]
3. Rename integration tests to `Test<Client>Daemon_<Behavior>`. [done]
4. Update `ARCHITECTURE.md` and the docs that name the old tests. [done]
5. Verify compile, skip paths, and real-daemon runs. [done]
6. Phase 2: rename fakes, share the logger fake, and fix off-goroutine `require`. [done]
7. Phase 2: move tests to the files of the code they test and rename subjects. [done]
8. Phase 2: convert the remaining stdlib assertions and align import groups. [done]

## decision log

- Client axis for files, not scenario axis. Unit tests and the documented
  `-run '^TestQbit'` commands are already per client.
- `Daemon` in the subject separates integration tests from unit tests in `-v`
  output. `-run 'Daemon_'` selects only integration tests, and
  `-run '^TestQbit'` still selects both.
- Removed `TestQbitPartialRawBehavior_ReportsPausedMissingFileState`. It only
  logged a result. `TestQbitDaemon_RecoversMisclassifiedCompletePack` asserts
  the same daemon behavior through the adapter: the recheck stage runs only
  when qBittorrent reports `missingFiles` for a paused skip-check add.
- Removed `TestTransmissionClient_ListsTorrentFiles`. It read whatever the
  daemon held and passed with no torrents. Each client's complete-pack test now
  asserts that `GetTorrents` and `GetFiles` report the imported pack.
- Cleanups use a context without cancellation, because `t.Context()` is
  canceled before cleanup functions run. The old Deluge removal used the
  canceled context and still worked, because go-deluge did not check it. The
  new rule does not depend on that.
- Complete packs now use 1 MiB episodes like partial packs (before: 1 byte).
  One pack writer serves both, and every client checks complete packs of
  real size.
- `transmission_test.go` and `client_test.go` moved from stdlib assertions to
  testify, so the whole `torrentclient` package follows the documented rule.
- Cleanup is registered before `Import`, so a failed import cannot leave a
  torrent that blocks the next run.

- `require` inside `httptest` handlers and fixture callbacks (56 calls in
  `cmd`, `http`, and `prowlarr`) became `assert`. `FailNow` from a handler
  goroutine ends that goroutine, not the test, so the failure report could be
  wrong.
- Declarations moved with a script that copies them byte for byte, so every
  moved test keeps its exact body.
- `cli_test.go` builds the binary once per test with `buildCLI`. The search
  CLI test used `go run` three times.
- Review follow-up: each `import<Client>Pack` cleanup asserts with
  `assertRemoved` that the daemon no longer holds the torrent. This replaces
  the old Deluge check that the whole daemon held exactly one torrent, which
  fails on a daemon with other torrents. A run with the qBittorrent removal
  disabled fails with "daemon still holds the torrent after removal".
- Review follow-up: the Deluge removal check reads the session list. Deluge 1.3
  returns an empty status for a removed hash, which go-deluge cannot decode.
- Review follow-up: Deluge pack names include the client type, so Deluge 1 and
  Deluge 2 runs on one import folder do not share pack folders.
- Review follow-up: `loggertest.Events` is a snapshot type with `Require` and
  `RequireField`, and field values are strings. JSON numbers decode as
  `float64`, so an `any` value could never match an `int`.
- `loggertest.Logger.Fatal` panics instead of exiting. No production code
  under test calls `Fatal`.

## verification

- `go vet ./internal/torrentclient` and `go vet -tags=integration
  ./internal/torrentclient` pass.
- With no `SEASONPACKARR_TEST_*` variables, all nine integration tests skip
  with one message format.
- Real daemons in Docker on one Linux volume: qBittorrent 5.2.3, Transmission
  4.1.3, Deluge 1.3.15, and Deluge 2.1.2. Every client passes twice in a row.
  The second run proves cleanup, because qBittorrent 5.2 rejects a duplicate
  add. After the runs, no daemon held a torrent.
- Partial packs check to exactly 0.33 on every client, so
  `assertPartialProgress` uses a 0.01 tolerance.
- `go test ./...` and `gofumpt -l .` pass. `govulncheck ./...` reports no called vulnerabilities.
- Phase 2: `go test -list '.*' ./...` lists 234 tests and benchmarks before and
  after. The diff contains only the planned renames. The integration list is
  unchanged.
- Phase 2: `go vet ./...`, `go vet -tags=integration ./internal/torrentclient`,
  `go test ./...`, `go test -race ./...`, and `gofumpt -l .` pass.
  `deadcode -test ./...` reports the same finding as before.
- Review follow-up: real daemons pass twice in a row on qBittorrent 5.2.3,
  Transmission 4.1.3, Deluge 1.3.15, and Deluge 2.1.2, with no torrent left
  behind. `go test -race ./...`, both `go vet` runs, and `gofumpt -l .` pass.
  The test list differs from phase 2 only by the planned renames and the split
  of the Deluge listing test.
