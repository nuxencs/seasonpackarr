# Parse owns the client import (supersedes PR #218)

## goal

Make torrent parsing the only supported season-pack path and move the
client re-import out of autobrr and into `POST /api/parse`, expressed through
the neutral `internal/torrentclient` abstraction so it works for both
qBittorrent and Transmission. This supersedes PR #218, which predated the
abstraction and drove raw `go-qbittorrent` types from inside `internal/http`.

## scope

- extend `torrentclient.TorrentClient` with `ImportDestination()` and `Import(ImportRequest)`
  plus stage-tagged `ImportError`; implement both on the qBittorrent and
  Transmission adapters behind private `qbitAPI` / `transmissionAPI` test seams
- replace per-client `preImportPath` with a neutral `import:` policy
  (`savePath`, `tags` + qBittorrent-only `category`, `downloadPath`,
  `contentLayout`); type-aware validation
- remove the `parseTorrentFile` toggle; make `/api/pack` a pure match gate and
  `/api/parse` recompute matches, hardlink, then import (no `matchMap` handoff)
- hard-fail deprecation guards for `parseTorrentFile` / `preImportPath` in
  config and env, in `New` and `DynamicReload`
- fix the env parser so multi-word client settings (`IMPORT_SAVE_PATH`) survive
- docs/schema/sample/env updates + migration notes

## non-goals

- changing the webhook payload shape or auth
- exposing every qBittorrent add option; only what the import needs

## risks

- **Breaking release.** The deprecation guards brick existing configs on
  upgrade until migrated — needs a major-version tag and a loud migration note.
- **Transmission verify cost.** Transmission has no skip-hash-check in its RPC
  (verified against 4.0.6 and 4.1.3), so every import hash-checks; timeouts are
  minutes, not seconds. The add auto-verifies; we force a verify and poll until
  it settles (`status == stopped && not checking`) before starting, which
  closes the premature-resume race.
- **Blocking import.** `/api/parse` blocks until the import settles; a slow
  verify can exceed autobrr's webhook timeout. Kept blocking for PR #218 parity
  and clean per-stage status codes; documented. Async is a possible follow-up.
- **Type-assertion-free interface.** Both adapters implement `ImportDestination`/`Import`
  on the core interface; a future read-only client would need a capability split.

## steps

1. Domain + abstraction: `ImportPolicy`, status codes 459–463, `torrents.InfoHashes`,
   `TorrentClient.ImportDestination/Import`, `ImportError`, `pollUntil`. [done]
2. Adapters: qBittorrent add/recheck/resume + import-root resolution;
   Transmission add/verify/poll/start. [done]
3. Config: policy, type-aware validation, deprecation guards, env `Cut` fix,
   embedded template. [done]
4. Processor: `collectMatches`, gate-only `/api/pack`, parse-owns-import
   `/api/parse`; remove `format.CleanAnnounceTitle`. [done]
5. Docs/schema/sample/compose. [done]
6. Gates + live verify against qBittorrent + Transmission. [in progress]

## decision log

- 2026-07-04: import is a first-class capability on `TorrentClient` (both
  clients implement it), config block named neutral `import:` — chosen over a
  qbit-only optional `Importer` to avoid a second migration when Transmission
  import shipped.
- 2026-07-04: no `pausedOnAdd` config knob. Both adapters always add the torrent
  stopped so the recheck/verify runs first, then always start it once the data
  checks out — a correct import is never left stopped, and adding it un-stopped
  would race the recheck. (Initially shipped as a `pausedOnAdd`="leave stopped"
  option; removed after live-proving always-start is safe for both clients on
  complete and partial packs.)
- 2026-07-04: per-stage `ImportError` → status codes 459–463 (config/add/find/
  recheck/resume), mapped by `torrentclient.ImportStatusCode`.
- 2026-08-08: the destination now includes the expected rooted or flat file
  layout. qBittorrent imports are pinned to the resolved save path, and an
  omitted `contentLayout` is read from qBittorrent preferences.
- 2026-08-08: hardlink creation is idempotent only when the existing target is
  the same inode as the source. This lets a failed client import be retried
  without accepting an unrelated file at the target path.
- 2026-08-08: torrent identity carries both the legacy SHA-1 identifier and the
  BEP 52 SHA-256 identifier. qBittorrent uses SHA-256 for pure v2 torrents and
  SHA-1 for v1 or hybrid torrents. Transmission uses its SHA-1 `hashString`.
- 2026-08-08: startup validation rejects unsupported qBittorrent content
  layouts. The JSON schema now expresses the same qBittorrent destination and
  Transmission-only field constraints as runtime validation.

## verification notes

- `gofumpt -w .`, `go fix ./...`, `go vet ./...` — clean
- `go test -race ./...` — all packages pass (adapter import machines tested
  against stub `qbitAPI` / `transmissionAPI` with millisecond timeouts;
  processor gate-only + parse-imports + cross-seed dedupe; config deprecation +
  env-parser + client validation)
- `govulncheck ./...` — no vulnerabilities in our code
- `go mod tidy` — no dependency changes
- `deadcode ./...` — only pre-existing unreachable funcs remain
  (`internal/http/health.go:writeUnhealthy`, `pkg/errors:Sentinel`,
  `pkg/errors:RecoverPanic`)
- Transmission add/verify semantics traced in Transmission 4.0.6 and 4.1.3
  source (`torrent.cc` `on_metainfo_completed` / `is_new_torrent_a_seed`):
  add auto-verifies regardless of the paused flag; paused only gates the
  post-verify auto-start.
- Live end-to-end verify against real qBittorrent (v5.x) + Transmission in
  Docker, both complete-pack and partial-pack:
  - complete pack: qbit `stalledUP progress=1.00`, transmission
    `status=seed percentDone=1.00`, no errors.
  - partial pack (1 of 3 episodes present): qbit `stalledDL progress=0.33`,
    transmission `status=download percentDone=0.33`, no errors — only the
    missing episodes are downloaded.
- BUG found and fixed via the live partial-pack test (had escaped both PR #218's
  and this port's stub tests): after a paused skip-check add, qBittorrent sits in
  `checkingResumeData` (with a misleading 100% progress) for ~1.5s before it
  flips to `missingFiles`. The original `waitForTorrent` returned on first
  appearance, caught `checkingResumeData`, skipped the recheck and resumed into
  an errored `missingFiles` torrent. Fixed by having `waitForTorrent` wait for
  the state to settle out of the transient checking states (new
  `isCheckingState` helper); regression-guarded by
  `TestQbitImportWaitsForCheckingToSettle` and the live partial-pack test.
