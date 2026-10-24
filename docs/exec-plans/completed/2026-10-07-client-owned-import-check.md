# Client-owned import check

## goal

`/api/import` must return before the torrent client finishes its data check.
A large partial season pack must not exceed autobrr's hard-coded 120-second
Webhook action timeout, and a cancelled request must not leave the torrent
stopped. Complete packs must still skip the full hash check where the client
supports it.

## production symptom

On 2026-10-07 a 10-episode 1080p pack with 9 hardlinked episodes failed with
`could not recheck torrent in client: context canceled` (status 461) after
exactly 120 seconds. autobrr v1.88.0 logged `Client.Timeout exceeded while
awaiting headers`. qBittorrent 5.1.4 finished the recheck 51 seconds later and
stopped the torrent because `forceRecheck` on a stopped torrent sets the
`FilesChecked` stop condition. The torrent stayed stopped until a manual start
about six hours later.

## scope

- decide before the add whether every torrent file is already on disk with the
  expected size (`ImportRequest.DataComplete`)
- qBittorrent: complete packs keep the skip-check add; partial packs use a
  normal started add and return when the torrent appears
- Transmission: add started, without a forced verify, and return
- Deluge: return after the resume without waiting for the check
- finish client mutations even when the HTTP caller disconnects
- update design docs, the timeout audit, and the tech-debt tracker

## non-goals

- a persisted asynchronous import job queue
- confirming the final client check result in seasonpackarr
- configuration changes

## risks

- a skip-check add trusts file sizes only; this is unchanged from the current
  qBittorrent behavior
- the completeness stat must use the same path the client uses; hardlinking
  already depends on this
- a started add must not let the client download before its check; verified
  against real clients before release
- qBittorrent can still report `missingFiles` after a complete skip-check add
  if a file changes between the stat and the add; the fallback must not wait
  for the recheck or leave the torrent stopped
- an early return hides client-side check or disk errors from the API caller;
  the client shows them

## steps

1. Reproduce and record the production failure. [done]
2. Probe qBittorrent 5.1.4 add strategies against a real daemon. [done]
3. Verify client source claims independently. [done]
4. Implement `DataComplete` and the adapter changes with unit tests. [done]
5. Run the integration tests against real qBittorrent, Transmission, and
   Deluge daemons, including a pack large enough to observe the check. [done]
6. Update docs, review the diff, and complete this plan. [done]

## decision log

- Raising `recheckTimeout` does not help: autobrr's limit is lower and fixed.
- Rejected: start a qBittorrent torrent in `missingFiles`. `start()` reloads it
  with the original seed-mode parameters and it returns to `missingFiles`.
- Rejected: recheck then start immediately on qBittorrent. `start()` does not
  clear the `FilesChecked` stop condition, so the torrent stops after the check.
- Rejected: persisted job queue. Every supported client can own the check and
  start by itself, so seasonpackarr has no long-running work to persist.
- Transmission needs no completeness input: its add path makes a complete pack
  with older mtimes a seed after a first-piece check, and verifies then starts
  anything else. The forced verify was removed.
- Rejected after source review: Deluge 2 `seed_mode` for complete packs. A
  rejected seed-mode add becomes a forced error state that `resume` cannot
  clear. Deluge keeps its normal check, which it always had.
- qBittorrent complete-pack fallback on `missingFiles`: `recheck`, `stop`,
  `start`. `stop` clears the `FilesChecked` stop condition that `recheck` sets
  on a stopped torrent, so no wait is needed. Deleting and re-adding the
  torrent was rejected because it could remove a torrent that the user owns.

## verification notes

Source review (independent agent, 2026-10-07): qBittorrent 5.1.4 and 4.4-4.6
WebAPI add parameters, `forceRecheck`, `start`, `stop`, and the checked-alert
handler; go-qbittorrent v1.17.0 `Prepare`; Transmission 4.0.3 and 4.1.3 add,
seed detection, and verify-done start; hekmon transmissionrpc v3 `paused`
serialization; Deluge 1.3.15 and 2.1.1 add, resume, and seed-mode rejection;
go-deluge v1.4.0 option serialization; libtorrent 1.2 and 2.0 seed-mode
resume checks.

Probe on qBittorrent 5.1.4 (3-episode pack, 1 episode on disk):

| Add strategy | Final state without more API calls |
| --- | --- |
| normal add, `stopped=false`, `stopCondition=None` | checking, then `stalledDL` 0.33 |
| same, complete pack | `stalledUP` 1.00 |
| skip-check stopped, recheck | `stoppedDL` 0.33 |
| skip-check stopped, recheck, start | `stoppedDL` 0.33 |
| skip-check stopped, start in `missingFiles` | `missingFiles` |
| skip-check stopped, recheck, stop, start | `stalledDL` 0.75 (2 GiB pack) |

Adapter timing with a 2 GiB pack (4 episodes hardlinked from two-day-old
files; partial packs have 3):

| Client | Complete pack | Partial pack |
| --- | --- | --- |
| qBittorrent 5.1.4 (save path, download path, category + Auto TMM) | 0.76-1.0 s, no check, `stalledUP` 1.00 | 4-256 ms while checking, `stalledDL` 0.75 |
| qBittorrent 5.1.4 with global "add stopped" and "stop after check" | same | same |
| Transmission 4.0.6 / 4.1.3 | 8-13 ms, seeding, no verify | 2-5 ms while checking, downloading 0.75 |
| Deluge 2.1.2 | 8 ms while checking, then seeding | 58 ms while checking, then 75% |
| Deluge 1.3.15 | 316 ms while checking, then 100% | 314 ms while checking, then 75% |
| qBittorrent fallback (`DataComplete` wrong) | 511 ms, then `stalledDL` 0.75 | n/a |

End-to-end through `seasonpackarr start` and `test import` with qBittorrent
5.1.4, category plus Auto TMM, on a Linux Docker volume:

| Build | Partial pack request | Complete pack request | Final states |
| --- | --- | --- | --- |
| this change | 0.12-0.40 s (4 packs) | 0.87 s | `stalledDL` 0.75 / `stalledUP` 1.00 |
| previous | 4.40 s (2 packs) | 1.40 s | same |

The same end-to-end run through Docker Desktop's macOS file sharing produced
transient `file_stat ... Operation not permitted` errors in qBittorrent for
hardlinks created milliseconds before the add. The Linux volume run did not, so
this is a test-environment artifact.

Repository checks: `go test ./...`, `go vet ./...`, `go vet -tags=integration
./internal/torrentclient`, `gofumpt -l .`, and `govulncheck ./...` pass.
`deadcode ./...` reports the same four pre-existing findings. The tagged
integration suites pass against qBittorrent 5.1.4, Transmission 4.0.6 and
4.1.3, Deluge 1.3.15, and Deluge 2.1.2.

`TestQbitImport_RecoversMisclassifiedCompletePack` pins the fallback against a
real daemon. Without the `stop` call it fails with the torrent at `stoppedDL`.

Contract note: a Deluge wait failure now reports the `resume` stage instead of
`recheck`, because the adapter no longer waits for the check.

Version matrix (added after review): the qBittorrent integration suite passes
twice in a row against 4.3.9, 4.4.5, 4.5.5, 4.6.7, 5.0.5, 5.1.4, and 5.2.3. A
2 GiB probe with a category, Auto TMM, and every global "start paused/stopped"
and "stop after check" preference enabled gave the same results on all seven:

| Case | Result on 4.3.9 to 5.2.3 |
| --- | --- |
| complete pack | 0.26-1.5 s, no hash check, `stalledUP` 1.00 |
| partial, last episode missing | 3-255 ms while checking, `stalledDL` 0.75 |
| partial, first episode missing | 2-8 ms while checking, `stalledDL` 0.75 |
| `DataComplete` wrong (fallback) | 0.76-1.0 s, `stalledDL` 0.75 |

Real download (a second client seeding the full pack over the Docker
network): qBittorrent 4.3.9 and 5.1.4, and Transmission 4.0.6 and 4.1.3 with
`incomplete-dir-enabled` and `start-added-torrents` off, downloaded the missing
episode with the first or the last episode missing and finished at 100%. All
files ended in the final folder, and the reused episodes kept the inode of
their source files.

`TestQbitImportDestination_UsesDaemonPreferences/manual_category_path` now
skips on qBittorrent before 4.5, which has no "use category paths in manual
mode" preference. qBittorrent 4.3.9 in manual mode saves a category torrent to
the default save path, which is what seasonpackarr resolves.
`TestQbitImport_ImportsCompletePack` removes its torrent on cleanup because
qBittorrent 5.2 rejects a duplicate add.
