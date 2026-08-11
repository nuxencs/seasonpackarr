# Torrent Client File Read Performance

Research date: 2026-08-11

## Decision

Add one bulk read contract at the `torrentclient` boundary. Suggested shape:

```go
type FileResult struct {
	Hash  string
	Files []File
	Err   error
}

GetFiles(hashes []string) []FileResult
```

Return exactly one result for each input, in input order. A missing torrent is a
per-hash error. A qBittorrent request error applies only to its hash. A
Transmission or Deluge whole-call error is copied into every requested result.
Successful results stay usable when another result fails. This preserves the
current planner behavior and candidate order even when a client returns a map
or a different order. Match returned hashes case-insensitively, but keep the
input spelling in `FileResult.Hash`.

Recommended implementations:

| Client | This PR | Later option |
| --- | --- | --- |
| qBittorrent | Run the existing single-hash request with a bounded worker pool. Start with 4 workers. | For qBittorrent 5.2 or later, add wrapper support for one `torrents/info` request with `includeFiles=true`. |
| Transmission | Send one `TorrentGetHashes` request for all candidate hashes. Request only `hashString` and `files`. | Chunk only after measured response-size or timeout failures. |
| Deluge v2 | Send one serialized `TorrentsStatus` request with all candidate hashes. | Add a `go-deluge` API that accepts status keys, then request only `hash` and `files`. |
| Deluge v1 | Read `SessionState`, then send one serialized `TorrentsStatus` request with the known, unique candidate hashes. | Add an explicit-key wrapper API that handles unknown IDs without decoding empty status dictionaries. |

Ship this client-read change before parse-once episode indexing. The changes
meet at the plan builder, but stay separate behind the torrent-client and
release matcher seams.

## qBittorrent

The pinned `github.com/autobrr/go-qbittorrent` v1.17.0 implementation exposes
only a single-hash file read. `GetFilesInformationCtx` sends
`GET torrents/files?hash=<hash>`, returns 404 for a missing torrent, and decodes
one file list ([source](https://github.com/autobrr/go-qbittorrent/blob/ba1163d4a82164d71adb57123049f809f3b71855/methods.go#L1431-L1460)). Its
filter options support many hashes for `torrents/info`, but do not expose
`includeFiles`, and the pinned `Torrent` model has no `files` member
([options](https://github.com/autobrr/go-qbittorrent/blob/ba1163d4a82164d71adb57123049f809f3b71855/domain.go#L486-L496),
[model](https://github.com/autobrr/go-qbittorrent/blob/ba1163d4a82164d71adb57123049f809f3b71855/domain.go#L57-L117)).

Bounded concurrency is the compatible path. The wrapper uses one shared
`http.Client`, has no request-wide mutex, and configures connection reuse with
10 idle connections per host
([source](https://github.com/autobrr/go-qbittorrent/blob/ba1163d4a82164d71adb57123049f809f3b71855/qbittorrent.go#L20-L31),
[transport](https://github.com/autobrr/go-qbittorrent/blob/ba1163d4a82164d71adb57123049f809f3b71855/qbittorrent.go#L92-L115)).
The Go `http.Client` contract supports concurrent use
([documentation](https://pkg.go.dev/net/http#Client)). Keep the pool below the
wrapper's per-host connection setting. Four workers limit qBittorrent CPU,
response memory, and retry bursts while removing serial round trips.

qBittorrent 5.2 adds the better server-side batch option. Web API 2.11.8 added
`includeFiles` to `torrents/info`
([changelog](https://github.com/qbittorrent/qBittorrent/blob/b2270f7f6fec1b10117564f642b961621ae0058a/WebAPI_Changelog.md#L90-L100)).
The endpoint accepts pipe-separated hashes, iterates the session's torrent
list, and includes files only when metadata exists
([implementation](https://github.com/qbittorrent/qBittorrent/blob/b2270f7f6fec1b10117564f642b961621ae0058a/src/webui/api/torrentscontroller.cpp#L586-L640)).
Consequences:

- Response order is session order, not request order.
- Unknown hashes are omitted.
- A magnet without metadata can be returned without a `files` field.
- The response includes the full torrent summary plus files. It costs more
  payload per torrent than `torrents/files`.
- The GET query grows by about 41 bytes per v1 hash, before URL encoding.
  Chunking is required if large candidate sets meet proxy URI limits.

Do not add a private raw HTTP implementation in seasonpackarr. The pinned
wrapper owns authentication, cookies, retries, and endpoint construction. Add
or adopt wrapper support first, then gate this path on Web API 2.11.8. Keep the
worker-pool fallback for older qBittorrent releases.

Partial-error rule for this PR: do not cancel other workers after one failure.
Write each success or error into its preassigned result index. The planner logs
and skips failed results, then consumes successes in original candidate order.

## Transmission

The pinned `github.com/hekmon/transmissionrpc/v3` v3.0.0 already implements the
required batch. `TorrentGetHashes` passes the complete hash slice as the RPC
`ids` array in one `torrent-get` call
([source](https://github.com/hekmon/transmissionrpc/blob/a9d6476918d29167ccda86b94ba728616e5af53d/torrent_accessors.go#L51-L57),
[encoding](https://github.com/hekmon/transmissionrpc/blob/a9d6476918d29167ccda86b94ba728616e5af53d/torrent_accessors.go#L92-L116)).
The model supports both `hashString` and `files`
([source](https://github.com/hekmon/transmissionrpc/blob/a9d6476918d29167ccda86b94ba728616e5af53d/torrent_accessors.go#L141-L149)).

Use:

```go
TorrentGetHashes(ctx, []string{"hashString", "files"}, hashes)
```

Transmission 4.0.3 accepts a mixed list of IDs and hashes. For a list, it walks
the request in order and appends only torrents that exist
([RPC contract](https://github.com/transmission/transmission/blob/6b0e49bbb296f1c84785275b8a8f18b4210180af/docs/rpc-spec.md#L123-L130),
[selection code](https://github.com/transmission/transmission/blob/6b0e49bbb296f1c84785275b8a8f18b4210180af/libtransmission/rpcimpl.cc#L88-L119)).
The response is built in that selected order
([source](https://github.com/transmission/transmission/blob/6b0e49bbb296f1c84785275b8a8f18b4210180af/libtransmission/rpcimpl.cc#L919-L957)).
Build an internal response map by a lower-case `hashString`, then emit one
result for each input hash. This detects omitted hashes and avoids relying on
server ordering.

One RPC has one success or failure result. It has no per-torrent error field.
Unknown hashes are omissions, not request errors. Treat every requested hash
that is absent from the response as a missing-torrent error. For a transport,
authentication, decode, or RPC result error, return that error in every input
result.

Payload is close to the aggregate serial payload, plus one `hashString` per
torrent. It removes repeated HTTP headers, JSON envelopes, and round trips.
The library decodes the complete response into a torrent slice. Do not add
concurrency. Start with one batch and the existing 60-second context. Add
chunks only if live measurements show large responses or timeouts.

The library itself can support concurrent HTTP calls. It uses a pooled HTTP
client and locks its random tag source and session ID
([source](https://github.com/hekmon/transmissionrpc/blob/a9d6476918d29167ccda86b94ba728616e5af53d/controller.go#L28-L68),
[locks](https://github.com/hekmon/transmissionrpc/blob/a9d6476918d29167ccda86b94ba728616e5af53d/controller.go#L71-L109)).
That capability is not useful here because the protocol already provides the
single-request batch.

## Deluge

The pinned `github.com/autobrr/go-deluge` v1.4.0 already exposes a semantic
batch. `TorrentsStatus` sends one `core.get_torrents_status` call with an `id`
filter containing all requested hashes
([source](https://github.com/autobrr/go-deluge/blob/245951c9058483f9637d5e1a0f5ac11941c89828/torrent_status.go#L162-L180)).
It returns a map keyed by hash
([source](https://github.com/autobrr/go-deluge/blob/245951c9058483f9637d5e1a0f5ac11941c89828/torrent_status.go#L185-L212)).

The main limitation is payload. The method always requests the wrapper's full
status list. That list includes `files`, file progress, priorities, peers, and
many summary fields
([source](https://github.com/autobrr/go-deluge/blob/245951c9058483f9637d5e1a0f5ac11941c89828/torrent_status.go#L85-L129)).
For this PR, one compressed full-status response is still preferable to one
full-status response per candidate. The next improvement is a wrapper method
that accepts explicit status keys. Deluge core already accepts a `keys` list
and applies it to all selected torrents
([Deluge 2.1.1](https://github.com/deluge-torrent/deluge/blob/0b5f45b486e8e974ba8a0b1d6e8edcd124fca62a/deluge/core/core.py#L764-L779),
[Deluge 1.3.15](https://github.com/deluge-torrent/deluge/blob/a6e8ac8725c2be28679e26b7c6674aad339338b1/deluge/core/core.py#L456-L468)).
The ideal request uses only `hash` and `files`.

Do not call the native Deluge client concurrently. The pinned wrapper increments
an unprotected serial, writes to one connection, immediately reads one response,
and rejects a response with a different serial
([request path](https://github.com/autobrr/go-deluge/blob/245951c9058483f9637d5e1a0f5ac11941c89828/delugeclient.go#L299-L315),
[read and validation](https://github.com/autobrr/go-deluge/blob/245951c9058483f9637d5e1a0f5ac11941c89828/delugeclient.go#L331-L449)).
Hold the existing `delugeClient.mu` for the complete bulk call.

Missing-hash behavior differs by daemon generation:

- Deluge 2.1.1 deletes unknown IDs before it returns the status dictionary
  ([source](https://github.com/deluge-torrent/deluge/blob/0b5f45b486e8e974ba8a0b1d6e8edcd124fca62a/deluge/core/torrentmanager.py#L1659-L1676)).
- Deluge 1.3.15 keeps the requested key and returns an empty status dictionary
  for a missing torrent
  ([source](https://github.com/deluge-torrent/deluge/blob/a6e8ac8725c2be28679e26b7c6674aad339338b1/deluge/core/core.py#L442-L468)).

The pinned wrapper tries to decode every v1 dictionary into a complete
`TorrentStatus`. An empty dictionary fails at the first required field before
the caller can inspect map presence. The v1 adapter therefore reads the small
session hash list first, removes unknown and duplicate IDs, then sends one bulk
status request for known hashes. Deluge v2 uses the direct bulk status request.
Both paths require a non-empty `TorrentStatus.Hash`, match hashes
case-insensitively, and emit results in input order. For a whole-call error,
return that error in every input result.

For Deluge v2, the wrapper reads the complete compressed frame into memory
before rencode decoding
([source](https://github.com/autobrr/go-deluge/blob/245951c9058483f9637d5e1a0f5ac11941c89828/delugeclient.go#L367-L407)).
This makes the explicit-key wrapper improvement valuable if candidate sets or
file lists become large. Until then, one serialized call is the simplest
end-to-end improvement.

## Verification Targets

- Shared contract test: successful entries remain usable when one hash fails.
- Shared contract test: output processing follows candidate order.
- qBittorrent: worker limit, mixed success and 404, no worker leak.
- Transmission: one API call, request fields are exactly `hashString` and
  `files`, missing hash omission, response order does not affect planning.
- Deluge v2: one API call under the mutex, unordered map, missing-hash omission,
  full-call error.
- Deluge v1: one session-state call plus one bulk status call under the mutex,
  unknown and duplicate filtering, full-call error.
- Plan builder: an error for one candidate does not discard successful
  candidates, matching stops after all eligible torrent targets are filled.
