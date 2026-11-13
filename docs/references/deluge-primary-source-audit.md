# Deluge primary-source audit

This note records the source facts that constrain Deluge support and its integration tests.

## Seasonpackarr implementation outcome

Seasonpackarr selects `deluge-v1` or `deluge-v2` explicitly and keeps
`deluge` as a V2 alias. It maps at most one configured import tag to the
optional Label plugin. It imports go-deluge v1.4.0 as a normal Go module
dependency. No dependency source is copied into the seasonpackarr repository.

Environment-gated local tests connect to real Deluge 1.3.15 and 2.1.2 daemons.
Both entries run complete and partial imports, path and file reads, initial
checks, resume, missing-label creation, and label assignment. Daemon fixtures
and test data are not stored in this repository. These tests are not part of
CI.

## Inspected revisions

- Autobrr: `9d3205c3d8abf259a20db224b646c1db7170986d`
  - Local clone: `/Users/nuxen/dev/oss/autobrr`
  - Upstream: <https://github.com/autobrr/autobrr/tree/9d3205c3d8abf259a20db224b646c1db7170986d>
- `autobrr/go-deluge` dependency used by seasonpackarr: v1.4.0 at `245951c9058483f9637d5e1a0f5ac11941c89828`
  - `go.mod` selects this release.
  - Local clone: `/Users/nuxen/dev/oss/go-deluge`
  - Upstream: <https://github.com/autobrr/go-deluge/tree/245951c9058483f9637d5e1a0f5ac11941c89828>
- `autobrr/go-deluge` local clone head: `1825ad22f4df1fb4c36ae359cf55cd16417216e9`
  - Local clone: `/Users/nuxen/dev/oss/go-deluge`
  - Upstream: <https://github.com/autobrr/go-deluge/tree/1825ad22f4df1fb4c36ae359cf55cd16417216e9>
  - Only Dependabot and CI workflow files differ from v1.4.0. The cited Go and shell sources are identical.
- Deluge development source: `e58075416dedd53636e89b1cd240f86f2e7c2ee0`
  - Local clone: `/Users/nuxen/dev/oss/deluge`
  - Upstream: <https://github.com/deluge-torrent/deluge/tree/e58075416dedd53636e89b1cd240f86f2e7c2ee0>
- Deluge 1.3.15 source: tag commit `a6e8ac8725c2be28679e26b7c6674aad339338b1`
  - Local source through `git show deluge-1.3.15:<path>` in `/Users/nuxen/dev/oss/deluge`
  - Upstream: <https://github.com/deluge-torrent/deluge/tree/a6e8ac8725c2be28679e26b7c6674aad339338b1>
- Deluge 2.2.0 source: tag commit `e9777eaabc8698473a05d8c02624f462bdc5af61`
  - Local source through `git show deluge-2.2.0:<path>` in `/Users/nuxen/dev/oss/deluge`
  - Upstream: <https://github.com/deluge-torrent/deluge/tree/e9777eaabc8698473a05d8c02624f462bdc5af61>
- qBittorrent 5.1.4: tag commit `33e5e772200b5e2f9d23af8870ce7436ec216faa`
  - Local clone: `/Users/nuxen/dev/oss/qBittorrent-history`
  - Upstream: <https://github.com/qbittorrent/qBittorrent/tree/33e5e772200b5e2f9d23af8870ce7436ec216faa>
- qBittorrent 5.2.0: tag commit `b2270f7f6fec1b10117564f642b961621ae0058a`
  - Local clone: `/Users/nuxen/dev/oss/qBittorrent-history`
  - Upstream: <https://github.com/qbittorrent/qBittorrent/tree/b2270f7f6fec1b10117564f642b961621ae0058a>
- `autobrr/go-qbittorrent` dependency used by seasonpackarr: v1.16.0 at `eb1f3ca0b17d3219f4b2bcc43199b664528f64ef`
  - `go.mod` selects this release.
  - Local clone: `/Users/nuxen/dev/oss/go-qbittorrent`
  - Upstream: <https://github.com/autobrr/go-qbittorrent/tree/eb1f3ca0b17d3219f4b2bcc43199b664528f64ef>

## V1 and V2 daemon selection

Autobrr does not detect the daemon protocol version. Its model has separate `DELUGE_V1` and `DELUGE_V2` client types. The action dispatch selects a separate V1 or V2 path from that configured type. Each path constructs `go-deluge` with `NewV1` or `NewV2`.

Sources:

- Autobrr client constants: [`internal/domain/client.go` lines 177-185](https://github.com/autobrr/autobrr/blob/9d3205c3d8abf259a20db224b646c1db7170986d/internal/domain/client.go#L177-L185). Local path: `/Users/nuxen/dev/oss/autobrr/internal/domain/client.go:177`.
- Autobrr action dispatch: [`internal/action/deluge.go` lines 18-43](https://github.com/autobrr/autobrr/blob/9d3205c3d8abf259a20db224b646c1db7170986d/internal/action/deluge.go#L18-L43). Local path: `/Users/nuxen/dev/oss/autobrr/internal/action/deluge.go:18`.
- Autobrr constructors: [`internal/action/deluge.go` lines 91-108](https://github.com/autobrr/autobrr/blob/9d3205c3d8abf259a20db224b646c1db7170986d/internal/action/deluge.go#L91-L108) and [`internal/action/deluge.go` lines 221-238](https://github.com/autobrr/autobrr/blob/9d3205c3d8abf259a20db224b646c1db7170986d/internal/action/deluge.go#L221-L238). Local path: `/Users/nuxen/dev/oss/autobrr/internal/action/deluge.go:91`.
- `go-deluge` constructors: [`delugeclient.go` lines 264-285](https://github.com/autobrr/go-deluge/blob/1825ad22f4df1fb4c36ae359cf55cd16417216e9/delugeclient.go#L264-L285). Local path: `/Users/nuxen/dev/oss/go-deluge/delugeclient.go:264`.

This selection is necessary because the V2 wire format adds a five-byte protocol header. The library says that the remote endpoint has no version handshake. It decides whether to read and write the header from the constructor-selected `v2daemon` flag.

Source: [`delugeclient.go` lines 296-297 and 331-381](https://github.com/autobrr/go-deluge/blob/1825ad22f4df1fb4c36ae359cf55cd16417216e9/delugeclient.go#L296-L381). Local path: `/Users/nuxen/dev/oss/go-deluge/delugeclient.go:296`.

Correction for seasonpackarr: a fixed `deluge.NewV2` constructor does not support Deluge 1.3. The configuration needs an explicit daemon version, or the connection code must try each protocol on a new connection. Autobrr uses explicit configuration.

## Labels and tags

Deluge has one Label plugin label per torrent. Autobrr does not map its `Tags` field to Deluge. It reads `Action.Label` and applies that one label after the torrent is added. This logic is the same for its V1 and V2 paths.

Sources:

- Autobrr keeps `Tags` and `Label` as separate action fields: [`internal/domain/action.go` lines 12-29](https://github.com/autobrr/autobrr/blob/9d3205c3d8abf259a20db224b646c1db7170986d/internal/domain/action.go#L12-L29). Local path: `/Users/nuxen/dev/oss/autobrr/internal/domain/action.go:12`.
- V1 label application after a magnet or file add: [`internal/action/deluge.go` lines 128-144](https://github.com/autobrr/autobrr/blob/9d3205c3d8abf259a20db224b646c1db7170986d/internal/action/deluge.go#L128-L144) and [`internal/action/deluge.go` lines 174-190](https://github.com/autobrr/autobrr/blob/9d3205c3d8abf259a20db224b646c1db7170986d/internal/action/deluge.go#L174-L190). Local path: `/Users/nuxen/dev/oss/autobrr/internal/action/deluge.go:128`.
- V2 label application after a magnet or file add: [`internal/action/deluge.go` lines 258-274](https://github.com/autobrr/autobrr/blob/9d3205c3d8abf259a20db224b646c1db7170986d/internal/action/deluge.go#L258-L274) and [`internal/action/deluge.go` lines 303-319](https://github.com/autobrr/autobrr/blob/9d3205c3d8abf259a20db224b646c1db7170986d/internal/action/deluge.go#L303-L319). Local path: `/Users/nuxen/dev/oss/autobrr/internal/action/deluge.go:258`.

`go-deluge` returns a Label plugin client only when the plugin is already enabled. Autobrr does not call `EnablePlugin("Label")` in its Deluge action. If the plugin is disabled, `LabelPlugin` returns `nil` and Autobrr skips assignment. Autobrr therefore supports labels, but it does not enable label support on the daemon.

Sources:

- Enabled-plugin check: [`plugins.go` lines 22-43](https://github.com/autobrr/go-deluge/blob/1825ad22f4df1fb4c36ae359cf55cd16417216e9/plugins.go#L22-L43). Local path: `/Users/nuxen/dev/oss/go-deluge/plugins.go:22`.
- Autobrr only acts when the returned plugin is not `nil`: [`internal/action/deluge.go` lines 133-143](https://github.com/autobrr/autobrr/blob/9d3205c3d8abf259a20db224b646c1db7170986d/internal/action/deluge.go#L133-L143). Local path: `/Users/nuxen/dev/oss/autobrr/internal/action/deluge.go:133`.

Autobrr first calls `label.set_torrent`. If Deluge returns `Unknown Label`, Autobrr calls `label.add` and retries `label.set_torrent`. It therefore creates a missing label definition when the Label plugin is enabled.

Sources:

- Autobrr create-and-retry logic: [`internal/action/deluge.go` lines 198-219](https://github.com/autobrr/autobrr/blob/9d3205c3d8abf259a20db224b646c1db7170986d/internal/action/deluge.go#L198-L219). Local path: `/Users/nuxen/dev/oss/autobrr/internal/action/deluge.go:198`.
- `go-deluge` RPC method mapping: [`plugins.go` lines 45-80](https://github.com/autobrr/go-deluge/blob/1825ad22f4df1fb4c36ae359cf55cd16417216e9/plugins.go#L45-L80). Local path: `/Users/nuxen/dev/oss/go-deluge/plugins.go:45`.
- Deluge 2 Label plugin validation and storage: [`deluge/plugins/Label/deluge_label/core.py` lines 171-196](https://github.com/deluge-torrent/deluge/blob/e58075416dedd53636e89b1cd240f86f2e7c2ee0/deluge/plugins/Label/deluge_label/core.py#L171-L196) and [`lines 304-329`](https://github.com/deluge-torrent/deluge/blob/e58075416dedd53636e89b1cd240f86f2e7c2ee0/deluge/plugins/Label/deluge_label/core.py#L304-L329). Local path: `/Users/nuxen/dev/oss/deluge/deluge/plugins/Label/deluge_label/core.py:171`.
- Deluge 1.3.15 exposes the same one-label operations: [`deluge/plugins/label/label/core.py` lines 180-204](https://github.com/deluge-torrent/deluge/blob/a6e8ac8725c2be28679e26b7c6674aad339338b1/deluge/plugins/label/label/core.py#L180-L204) and [`lines 307-327`](https://github.com/deluge-torrent/deluge/blob/a6e8ac8725c2be28679e26b7c6674aad339338b1/deluge/plugins/label/label/core.py#L307-L327). Local source: `git show deluge-1.3.15:deluge/plugins/label/label/core.py` in `/Users/nuxen/dev/oss/deluge`.

The Label plugin converts new label IDs to lower case and only accepts `[a-z0-9_-]`. A seasonpackarr `import.tags` list cannot map losslessly to Deluge labels. The implementation should expose one Deluge label or define a clear one-value conversion rule.

## Add, check, recheck, and resume

Autobrr sets `add_paused` on every Deluge add. It maps `SavePath` to `download_location`. It does not request a force recheck or wait for a check.

Source: [`internal/action/deluge.go` lines 327-349](https://github.com/autobrr/autobrr/blob/9d3205c3d8abf259a20db224b646c1db7170986d/internal/action/deluge.go#L327-L349). Local path: `/Users/nuxen/dev/oss/autobrr/internal/action/deluge.go:327`.

In Deluge, `add_paused` only controls whether Deluge calls `torrent.resume()` after the add. Deluge sends the torrent metadata and `download_location` to libtorrent as `save_path`. Deluge does not call `force_recheck` in this add path.

Sources:

- Build and pass add parameters: [`deluge/core/torrentmanager.py` lines 422-492](https://github.com/deluge-torrent/deluge/blob/e58075416dedd53636e89b1cd240f86f2e7c2ee0/deluge/core/torrentmanager.py#L422-L492). Local path: `/Users/nuxen/dev/oss/deluge/deluge/core/torrentmanager.py:422`.
- Resume only when `add_paused` is false: [`deluge/core/torrentmanager.py` lines 624-659](https://github.com/deluge-torrent/deluge/blob/e58075416dedd53636e89b1cd240f86f2e7c2ee0/deluge/core/torrentmanager.py#L624-L659). Local path: `/Users/nuxen/dev/oss/deluge/deluge/core/torrentmanager.py:624`.
- Deluge 1.3.15 has the same add-paused control: [`deluge/core/torrentmanager.py` lines 460-505](https://github.com/deluge-torrent/deluge/blob/a6e8ac8725c2be28679e26b7c6674aad339338b1/deluge/core/torrentmanager.py#L460-L505). Local source: `git show deluge-1.3.15:deluge/core/torrentmanager.py` in `/Users/nuxen/dev/oss/deluge`.

Both Deluge 1.3 and Deluge 2 export `core.force_recheck`. The Deluge 2 implementation records whether the torrent was paused, calls libtorrent `force_recheck`, resumes the handle for the check, and restores the paused state on the checked alert. Deluge 1.3 follows the same pattern.

Sources:

- Deluge 2 exported RPC method: [`deluge/core/core.py` lines 937-941](https://github.com/deluge-torrent/deluge/blob/e58075416dedd53636e89b1cd240f86f2e7c2ee0/deluge/core/core.py#L937-L941). Local path: `/Users/nuxen/dev/oss/deluge/deluge/core/core.py:937`.
- Deluge 2 force-recheck implementation: [`deluge/core/torrent.py` lines 1463-1478](https://github.com/deluge-torrent/deluge/blob/e58075416dedd53636e89b1cd240f86f2e7c2ee0/deluge/core/torrent.py#L1463-L1478). Local path: `/Users/nuxen/dev/oss/deluge/deluge/core/torrent.py:1463`.
- Deluge 2 restores a prior paused state after the checked alert: [`deluge/core/torrentmanager.py` lines 1335-1348](https://github.com/deluge-torrent/deluge/blob/e58075416dedd53636e89b1cd240f86f2e7c2ee0/deluge/core/torrentmanager.py#L1335-L1348). Local path: `/Users/nuxen/dev/oss/deluge/deluge/core/torrentmanager.py:1335`.
- Deluge 1.3.15 exported RPC method: [`deluge/core/core.py` lines 551-555](https://github.com/deluge-torrent/deluge/blob/a6e8ac8725c2be28679e26b7c6674aad339338b1/deluge/core/core.py#L551-L555). Local source: `git show deluge-1.3.15:deluge/core/core.py`.
- Deluge 1.3.15 force-recheck implementation: [`deluge/core/torrent.py` lines 1009-1031](https://github.com/deluge-torrent/deluge/blob/a6e8ac8725c2be28679e26b7c6674aad339338b1/deluge/core/torrent.py#L1009-L1031). Local source: `git show deluge-1.3.15:deluge/core/torrent.py`.

`go-deluge` does not expose `ForceRecheck` in its `DelugeClient` interface or methods at the inspected revision. Its support table also marks `core.force_recheck` as unsupported. It exposes resume only. An implementation that promises an explicit recheck needs a `go-deluge` extension or another native RPC implementation.

Sources: [`delugeclient.go` lines 63-94](https://github.com/autobrr/go-deluge/blob/245951c9058483f9637d5e1a0f5ac11941c89828/delugeclient.go#L63-L94) and [`README.md` lines 44-56](https://github.com/autobrr/go-deluge/blob/245951c9058483f9637d5e1a0f5ac11941c89828/README.md#L44-L56). Local paths: `/Users/nuxen/dev/oss/go-deluge/delugeclient.go:63` and `/Users/nuxen/dev/oss/go-deluge/README.md:44`.

Correction for seasonpackarr: `add_paused`, wait until the status is not `Checking`, then resume is invalid because Deluge reports `Paused` before the libtorrent state. With the unmodified upstream Go module, seasonpackarr instead adds paused, resumes, then waits until the torrent is no longer paused or checking. The real-daemon tests verify that complete and partial fixtures account for their present data through this initial-check path. Seasonpackarr does not claim that it performs an explicit force recheck.

Source for paused-state precedence: [`deluge/core/torrent.py` lines 650-670](https://github.com/deluge-torrent/deluge/blob/e58075416dedd53636e89b1cd240f86f2e7c2ee0/deluge/core/torrent.py#L650-L670). Local path: `/Users/nuxen/dev/oss/deluge/deluge/core/torrent.py:650`.

## Paths and file names

`download_location` is the active data root. Deluge passes it to libtorrent as `save_path`. Deluge 2 exposes `save_path` as a deprecated alias for `download_location`. The file list contains torrent-relative paths with `/` separators.

Sources:

- Option meaning: [`deluge/core/torrent.py` lines 118-149](https://github.com/deluge-torrent/deluge/blob/e58075416dedd53636e89b1cd240f86f2e7c2ee0/deluge/core/torrent.py#L118-L149). Local path: `/Users/nuxen/dev/oss/deluge/deluge/core/torrent.py:118`.
- `download_location` to libtorrent `save_path`: [`deluge/core/torrentmanager.py` lines 474-481](https://github.com/deluge-torrent/deluge/blob/e58075416dedd53636e89b1cd240f86f2e7c2ee0/deluge/core/torrentmanager.py#L474-L481). Local path: `/Users/nuxen/dev/oss/deluge/deluge/core/torrentmanager.py:474`.
- Status alias: [`deluge/core/torrent.py` lines 1133-1138](https://github.com/deluge-torrent/deluge/blob/e58075416dedd53636e89b1cd240f86f2e7c2ee0/deluge/core/torrent.py#L1133-L1138). Local path: `/Users/nuxen/dev/oss/deluge/deluge/core/torrent.py:1133`.
- File list conversion: [`deluge/core/torrent.py` lines 81-115](https://github.com/deluge-torrent/deluge/blob/e58075416dedd53636e89b1cd240f86f2e7c2ee0/deluge/core/torrent.py#L81-L115). Local path: `/Users/nuxen/dev/oss/deluge/deluge/core/torrent.py:81`.
- `go-deluge` copies V1 `SavePath` into `DownloadLocation` for a common API: [`torrent_status.go` lines 132-159](https://github.com/autobrr/go-deluge/blob/1825ad22f4df1fb4c36ae359cf55cd16417216e9/torrent_status.go#L132-L159) and [`lines 162-212`](https://github.com/autobrr/go-deluge/blob/1825ad22f4df1fb4c36ae359cf55cd16417216e9/torrent_status.go#L162-L212). Local path: `/Users/nuxen/dev/oss/go-deluge/torrent_status.go:132`.

`move_completed_path` is a different option. It is only a destination used when `move_completed` is enabled. It must not replace `download_location` for an import that needs Deluge to find pre-existing data at add time.

Source: [`deluge/core/torrent.py` lines 136-137](https://github.com/deluge-torrent/deluge/blob/e58075416dedd53636e89b1cd240f86f2e7c2ee0/deluge/core/torrent.py#L136-L137).

## Duplicate add behavior

### Deluge

Duplicate behavior differs between Deluge daemon generations.

- Deluge 1.3.15 detects an existing torrent, can merge new trackers, and returns no torrent ID. `go-deluge` maps a nil RPC return to an empty hash and no error.
- Current Deluge 2 detects an existing torrent, can merge trackers, and raises `AddTorrentError("Torrent already in session (...)")`.
- `go-deluge` also documents that an add may return a nil value when the torrent was already added. Its add methods return an empty string for that nil value.

Sources:

- Deluge 1.3.15 duplicate branch: [`deluge/core/torrentmanager.py` lines 390-424](https://github.com/deluge-torrent/deluge/blob/a6e8ac8725c2be28679e26b7c6674aad339338b1/deluge/core/torrentmanager.py#L390-L424). Local source: `git show deluge-1.3.15:deluge/core/torrentmanager.py`.
- Deluge 2 duplicate branch: [`deluge/core/torrentmanager.py` lines 448-456](https://github.com/deluge-torrent/deluge/blob/e58075416dedd53636e89b1cd240f86f2e7c2ee0/deluge/core/torrentmanager.py#L448-L456). Local path: `/Users/nuxen/dev/oss/deluge/deluge/core/torrentmanager.py:448`.
- `go-deluge` add-file return decoding: [`methods.go` lines 115-138](https://github.com/autobrr/go-deluge/blob/1825ad22f4df1fb4c36ae359cf55cd16417216e9/methods.go#L115-L138). Local path: `/Users/nuxen/dev/oss/go-deluge/methods.go:115`.

Neither Deluge duplicate branch performs a data recheck. Each branch returns
or raises before the normal add parameters are built and before libtorrent
receives a new add. Seasonpackarr treats both the V1 empty result and the V2
`AddTorrentError` as a no-action result. It returns before label assignment,
resume, or status polling. No explicit recheck is part of duplicate handling.

### qBittorrent

qBittorrent detects the same info hash before its normal add setup. It can set missing metadata and merge trackers or web seeds, then it returns `false`. It does not apply the new add parameters and it does not force a data recheck.

Sources:

- qBittorrent 5.1.4 duplicate branch and early return: [`src/base/bittorrent/sessionimpl.cpp` lines 2743-2812](https://github.com/qbittorrent/qBittorrent/blob/33e5e772200b5e2f9d23af8870ce7436ec216faa/src/base/bittorrent/sessionimpl.cpp#L2743-L2812). Local source: `git show release-5.1.4:src/base/bittorrent/sessionimpl.cpp` in `/Users/nuxen/dev/oss/qBittorrent-history`.
- qBittorrent 5.2.0 duplicate branch and early return: [`src/base/bittorrent/sessionimpl.cpp` lines 2719-2781](https://github.com/qbittorrent/qBittorrent/blob/b2270f7f6fec1b10117564f642b961621ae0058a/src/base/bittorrent/sessionimpl.cpp#L2719-L2781). Local source: `git show release-5.2.0:src/base/bittorrent/sessionimpl.cpp` in `/Users/nuxen/dev/oss/qBittorrent-history`.
- Explicit recheck is a separate operation that calls libtorrent `force_recheck`: [`src/base/bittorrent/torrentimpl.cpp` lines 1665-1702](https://github.com/qbittorrent/qBittorrent/blob/b2270f7f6fec1b10117564f642b961621ae0058a/src/base/bittorrent/torrentimpl.cpp#L1665-L1702). Local source: `git show release-5.2.0:src/base/bittorrent/torrentimpl.cpp`.

The qBittorrent Web API result is version-dependent:

- qBittorrent 5.1.4 and earlier supported releases return the text `Fails.` with a successful HTTP response when every submitted torrent fails to add. An exact duplicate is therefore an HTTP-level success with a failure body.
- qBittorrent 5.2.0 uses a structured result. If a request has at least one success or pending add, it returns counts. If all submitted torrents fail, it throws `Conflict`, which the Web application maps to HTTP 409. A seasonpackarr request uploads one torrent, so an exact duplicate is HTTP 409 on this API.

Sources:

- qBittorrent 5.1.4 add result: [`src/webui/api/torrentscontroller.cpp` lines 880-906](https://github.com/qbittorrent/qBittorrent/blob/33e5e772200b5e2f9d23af8870ce7436ec216faa/src/webui/api/torrentscontroller.cpp#L880-L906). Local source: `git show release-5.1.4:src/webui/api/torrentscontroller.cpp`.
- qBittorrent 5.2.0 uploaded-file result and all-failure conflict: [`src/webui/api/torrentscontroller.cpp` lines 1242-1281](https://github.com/qbittorrent/qBittorrent/blob/b2270f7f6fec1b10117564f642b961621ae0058a/src/webui/api/torrentscontroller.cpp#L1242-L1281). Local source: `git show release-5.2.0:src/webui/api/torrentscontroller.cpp`.
- `Conflict` maps to an HTTP conflict error: [`src/webui/webapplication.cpp` lines 443-459](https://github.com/qbittorrent/qBittorrent/blob/b2270f7f6fec1b10117564f642b961621ae0058a/src/webui/webapplication.cpp#L443-L459). Local source: `git show release-5.2.0:src/webui/webapplication.cpp`.

The seasonpackarr dependency `go-qbittorrent` v1.16.0 does not inspect the legacy `Fails.` response body. It accepts HTTP 200 text as a successful add and returns `SuccessCount: 1`. It converts HTTP 409 to `ErrTorrentAddFailed`.

Source: [`methods.go` lines 639-676](https://github.com/autobrr/go-qbittorrent/blob/eb1f3ca0b17d3219f4b2bcc43199b664528f64ef/methods.go#L639-L676). Local source: `git show v1.16.0:methods.go` in `/Users/nuxen/dev/oss/go-qbittorrent`.

### Current seasonpackarr call order

The working tree inspected on 2026-08-08 has these paths:

- Request-level duplicate gate: `/api/candidate` checks the announce against the
  short-lived client inventory. `/api/pack` reuses that inventory while building
  its exact plan. `/api/parse` reuses the accepted plan or safely rebuilds it.
  If the same release is already present, processing returns
  `StatusAlreadyInClient` before file lookup, hardlink creation, or client
  import. The current wire status is `210`, a 2xx no-action response. An exact
  duplicate therefore does not reach any torrent-client duplicate-add path in
  the normal seasonpackarr flow. Local sources:
  `internal/http/processor_candidate.go`, `internal/http/processor_plan.go`, and
  `internal/release/release.go`.
- qBittorrent: build add options, call `AddTorrentFromMemory`, and return immediately on any library error. It only looks up the torrent after a successful library result. It rechecks only when the existing torrent state is `missingFiles`. It returns without a recheck when the existing torrent is active. Local source: `/Users/nuxen/dev/seasonpackarr/internal/torrentclient/qbittorrent.go:214-269`.
- Deluge: add paused. A V1 empty result or V2 `already in session` error returns
  without mutation. A newly added torrent receives the optional label, resumes,
  and waits for a started state. It does not force a recheck. Local source:
  `/Users/nuxen/dev/seasonpackarr/internal/torrentclient/deluge.go:203-253`.

Consequences:

- In the normal HTTP flow, an exact duplicate stops at the request-level gate.
  Seasonpackarr does not hardlink files, call the client adapter, or report a
  successful import.
- If the qBittorrent adapter directly receives a duplicate from qBittorrent
  5.1.4 or older, it treats the `Fails.` body as add success. It then
  conditionally rechecks only if qBittorrent already reports `missingFiles`.
- If the qBittorrent adapter directly receives a duplicate from qBittorrent
  5.2.0 or newer, it returns an add-stage error for HTTP 409. It never reaches
  lookup or recheck.
- If the Deluge adapter directly receives a duplicate from V1 or V2, it
  returns without applying a label, resuming, or polling the existing torrent.

The three adapter consequences apply only when an adapter is called directly,
or if a future flow bypasses the request-level duplicate gate. They do not
describe the current end-to-end behavior for a duplicate season-pack request.
No client rechecks merely because an exact duplicate was submitted. Duplicate
idempotency and data verification are separate adapter operations.

## Smallest `go-deluge` force-recheck addition

The smallest upstream-compatible API change has four parts:

1. Add `ForceRecheck(ctx context.Context, ids []string) error` to `DelugeClient`.
2. Add a `Client.ForceRecheck` method that sends one list argument to `core.force_recheck`, matching the existing `ForceReannounce` implementation.
3. Add one mock unit test for the successful RPC response. Both `Client` and `ClientV2` use the method because `ClientV2` embeds `Client`.
4. Mark `core.force_recheck` supported in the README table.

Both Deluge 1.3.15 and Deluge 2 use the same RPC method name and list argument. No constructor-specific implementation is needed. The slice signature matches the adjacent `ForceReannounce` method and the Deluge RPC contract.

Sources:

- Current interface and missing method: [`delugeclient.go` lines 63-94](https://github.com/autobrr/go-deluge/blob/245951c9058483f9637d5e1a0f5ac11941c89828/delugeclient.go#L63-L94). Local path: `/Users/nuxen/dev/oss/go-deluge/delugeclient.go:63`.
- Existing method pattern: [`methods.go` lines 436-450](https://github.com/autobrr/go-deluge/blob/245951c9058483f9637d5e1a0f5ac11941c89828/methods.go#L436-L450). Local path: `/Users/nuxen/dev/oss/go-deluge/methods.go:436`.
- V1 RPC contract: [`deluge/core/core.py` lines 551-555](https://github.com/deluge-torrent/deluge/blob/a6e8ac8725c2be28679e26b7c6674aad339338b1/deluge/core/core.py#L551-L555).
- V2 RPC contract: [`deluge/core/core.py` lines 937-941](https://github.com/deluge-torrent/deluge/blob/e58075416dedd53636e89b1cd240f86f2e7c2ee0/deluge/core/core.py#L937-L941).
- README support table: [`README.md` lines 44-56](https://github.com/autobrr/go-deluge/blob/245951c9058483f9637d5e1a0f5ac11941c89828/README.md#L44-L56). Local path: `/Users/nuxen/dev/oss/go-deluge/README.md:44`.

## Call serialization

The inspected `go-deluge` client increments a mutable request serial, writes one request, and then reads its response. It has no internal mutex. Seasonpackarr should serialize calls that share one client connection. This is a limitation of this Go client implementation, not a demonstrated limit of the Deluge server protocol.

Sources: [`delugeclient.go` lines 106-116](https://github.com/autobrr/go-deluge/blob/245951c9058483f9637d5e1a0f5ac11941c89828/delugeclient.go#L106-L116) and [`lines 299-356`](https://github.com/autobrr/go-deluge/blob/245951c9058483f9637d5e1a0f5ac11941c89828/delugeclient.go#L299-L356). Local path: `/Users/nuxen/dev/oss/go-deluge/delugeclient.go:106`.

## Torrent hashes

Deluge daemon V1/V2 describes the native RPC protocol generation. It does not describe BitTorrent v1/v2 metadata.

The Deluge 1.3.15 test suite computes torrent IDs as the SHA-1 hash of the bencoded `info` dictionary. Its old daemon stack is therefore a BitTorrent v1 test target. Deluge 2.2.0 added support for creating BitTorrent v2 torrents. No inspected `go-deluge` method restricts an ID string to 40 characters.

Sources:

- Deluge 1.3.15 SHA-1 test: [`deluge/tests/test_core.py` lines 157-170](https://github.com/deluge-torrent/deluge/blob/a6e8ac8725c2be28679e26b7c6674aad339338b1/deluge/tests/test_core.py#L157-L170). Local source: `git show deluge-1.3.15:deluge/tests/test_core.py`.
- Deluge 2.2.0 release note: [`CHANGELOG.md` lines 30-41](https://github.com/deluge-torrent/deluge/blob/e9777eaabc8698473a05d8c02624f462bdc5af61/CHANGELOG.md#L30-L41). Local source: `git show deluge-2.2.0:CHANGELOG.md`.
- `go-deluge` takes string IDs without length validation: [`delugeclient.go` lines 63-94](https://github.com/autobrr/go-deluge/blob/1825ad22f4df1fb4c36ae359cf55cd16417216e9/delugeclient.go#L63-L94). Local path: `/Users/nuxen/dev/oss/go-deluge/delugeclient.go:63`.

Correction for seasonpackarr: the broad claim that Deluge does not support pure BitTorrent v2 torrents is not supported by these sources. Deluge 1.3 cannot be the pure-v2 target. Deluge 2 support depends on the selected Deluge and libtorrent releases and must be verified with a pure-v2 fixture if seasonpackarr intends to accept it. A seasonpackarr policy may still reject pure v2, but the documentation must state that it is a seasonpackarr limitation.

## Native RPC integration-test setup

Use a two-entry matrix. Each entry must start a real daemon and run the seasonpackarr adapter against native RPC.

| Matrix entry | Daemon | Client constructor | Required fixture |
| --- | --- | --- | --- |
| `deluge-v1` | Deluge 1.3.15 with its compatible Python 2 and libtorrent stack | `deluge.NewV1` | BitTorrent v1 torrent |
| `deluge-v2` | A pinned Deluge 2 release, preferably the oldest supported release and optionally the newest supported release | `deluge.NewV2` | BitTorrent v1 torrent, plus pure v2 when claimed |

The daemon setup needs these parts:

1. Use a new config directory for each test process.
2. Before daemon start, write an auth entry with the `username:password:10` format. Auth level `10` is admin.
3. Start `deluged` in the foreground and wait for native RPC port `58846` to accept connections.
4. If the test and daemon share a network namespace, use the default localhost bind. If a container publishes the port to a host test process, set `allow_remote: true` or bind the RPC server to a non-loopback interface.
5. Select `NewV1` or `NewV2` from the matrix entry. Do not select from the reported daemon version because login already requires the correct protocol framing.
6. Enable the Label plugin in setup when the test covers labels. Confirm that `GetEnabledPlugins` contains `Label` before label assertions.
7. Use a local torrent fixture and local data. Do not depend on trackers, peers, or public downloads.
8. Remove the torrent without deleting fixture data during cleanup. Stop the daemon even when a test fails.

Sources:

- `go-deluge` upstream setup writes `localclient:deluge:10`, starts `deluged --do-not-daemonize`, waits on port `58846`, and selects separate V1/V2 test binaries: [`scripts/deluge-integration.sh` lines 16-52](https://github.com/autobrr/go-deluge/blob/1825ad22f4df1fb4c36ae359cf55cd16417216e9/scripts/deluge-integration.sh#L16-L52). Local path: `/Users/nuxen/dev/oss/go-deluge/scripts/deluge-integration.sh:16`.
- `go-deluge` pins Deluge 1.3.15 and its old libtorrent stack in its installer: [`scripts/deluge-install.sh` lines 47-63](https://github.com/autobrr/go-deluge/blob/1825ad22f4df1fb4c36ae359cf55cd16417216e9/scripts/deluge-install.sh#L47-L63). Local path: `/Users/nuxen/dev/oss/go-deluge/scripts/deluge-install.sh:47`.
- Deluge default RPC port and default loopback-only setting: [`deluge/core/preferencesmanager.py` lines 37-44](https://github.com/deluge-torrent/deluge/blob/e58075416dedd53636e89b1cd240f86f2e7c2ee0/deluge/core/preferencesmanager.py#L37-L44). Local path: `/Users/nuxen/dev/oss/deluge/deluge/core/preferencesmanager.py:37`.
- Deluge RPC bind behavior: [`deluge/core/rpcserver.py` lines 379-418](https://github.com/deluge-torrent/deluge/blob/e58075416dedd53636e89b1cd240f86f2e7c2ee0/deluge/core/rpcserver.py#L379-L418). Local path: `/Users/nuxen/dev/oss/deluge/deluge/core/rpcserver.py:379`.
- Deluge auth levels and auth-file creation: [`deluge/common.py` lines 1222-1269](https://github.com/deluge-torrent/deluge/blob/e58075416dedd53636e89b1cd240f86f2e7c2ee0/deluge/common.py#L1222-L1269). Local path: `/Users/nuxen/dev/oss/deluge/deluge/common.py:1222`.

The inspected `go-deluge` GitHub workflow does not run its real-daemon integration jobs. Both jobs are commented out. Its scripts are useful source examples, but they are not current CI evidence and include old package URLs. A seasonpackarr test must own and verify its pinned daemon environment.

Source: [`.github/workflows/go.yml` lines 32-70](https://github.com/autobrr/go-deluge/blob/1825ad22f4df1fb4c36ae359cf55cd16417216e9/.github/workflows/go.yml#L32-L70). Local path: `/Users/nuxen/dev/oss/go-deluge/.github/workflows/go.yml:32`.

Run each integration-test matrix entry from the repository root after starting the
matching daemon and setting its connection environment:

```sh
SEASONPACKARR_TEST_DELUGE_TYPE=deluge-v1 \
SEASONPACKARR_TEST_DELUGE_HOST=127.0.0.1 \
SEASONPACKARR_TEST_DELUGE_PORT=58846 \
SEASONPACKARR_TEST_DELUGE_USER=seasonpackarr \
SEASONPACKARR_TEST_DELUGE_PASS=integration \
SEASONPACKARR_TEST_IMPORT_DIR=/path/shared/with/deluge \
go test -tags=integration -v -count=1 \
  -run '^TestDelugeImport_ImportsAgainstDaemon$' ./internal/torrentclient
```

Repeat with `SEASONPACKARR_TEST_DELUGE_TYPE=deluge-v2` against a Deluge 2
daemon. The repository does not provide daemon images, static torrent data, or
a fixture runner.

## Fresh-daemon log classification

Fresh daemon config directories produce warnings for missing `core.conf`,
session state, DHT state, torrent state, fast-resume state, and Label config.
These files do not exist on the first start. Deluge creates them as it saves
state. A minimal test daemon configuration can also omit the optional GeoIP
database.

Deluge 1.3 logs an error when its first state save tries to back up a state
file that does not exist yet. The save that follows succeeds. This is a
first-run fixture condition, not a failed torrent import.

Deluge 2.1.2 can log `Torrent id not in torrents loading list` after
`core.add_torrent_file`. That RPC uses the synchronous torrent-manager `add`
path, which creates the torrent object directly. Libtorrent later emits an
`add_torrent_alert`. Its handler only looks in the `torrents_loading` map used
by `add_async`, logs the warning when the synchronously added torrent is not
there, and returns. The torrent is already present and the integration test
continues to verify its state, path, files, progress, and label.

Sources:

- Synchronous `core.add_torrent_file` call: [`deluge/core/core.py` lines 457-481](https://github.com/deluge-torrent/deluge/blob/e58075416dedd53636e89b1cd240f86f2e7c2ee0/deluge/core/core.py#L457-L481).
- Synchronous and asynchronous manager paths: [`deluge/core/torrentmanager.py` lines 497-626](https://github.com/deluge-torrent/deluge/blob/e58075416dedd53636e89b1cd240f86f2e7c2ee0/deluge/core/torrentmanager.py#L497-L626).
- Warning in the asynchronous alert handler: [`deluge/core/torrentmanager.py` lines 1247-1265](https://github.com/deluge-torrent/deluge/blob/e58075416dedd53636e89b1cd240f86f2e7c2ee0/deluge/core/torrentmanager.py#L1247-L1265).

## Minimum end-to-end assertions

Run these assertions against both matrix entries unless a daemon capability is version-specific:

1. Connect and assert the expected daemon major version.
2. Create source data in the configured `download_location`.
3. Add the local torrent paused and assert the returned or known hash.
4. Assert torrent listing, `download_location`, and torrent-relative file paths through seasonpackarr.
5. Resume and observe the normal initial check when it is long enough to sample. Wait until the torrent is no longer paused or checking, then assert expected completed bytes or progress.
6. Assert the torrent is not left paused.
7. With Label enabled, assign a missing label. Assert that Deluge created the lower-case label and assigned it to the torrent.
8. Stop and restart the daemon with the same state directory. Reconnect and assert that the torrent path and label persist.

These checks test the native protocol, daemon-version selection, filesystem
path semantics, initial-check behavior, and Label plugin behavior. Unit stubs
cannot verify these contracts. Unit tests cover the direct-adapter V1 and V2
duplicate no-action results because normal HTTP duplicate requests stop at the
request-level gate.
