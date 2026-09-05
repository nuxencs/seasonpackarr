# Release planning performance

## goal

Reduce exact season-pack planning CPU, allocations, and torrent-client round
trips without changing release compatibility or hardlink selection.

## scope

- parse each client and pack episode filename once per plan
- replace the candidate-by-target scan with an indexed exact match
- preserve deterministic target ordering and cross-seed deduplication
- research bulk file-detail reads in the exact qBittorrent, Transmission, and
  Deluge client revisions used by this repository
- design and implement the smallest torrent-client interface that lets each
  adapter use its strongest safe read strategy
- add behavior, call-count, and performance regression coverage

## non-goals

- changing resolution, group, container, size, season, or episode acceptance
- changing candidate discovery or fuzzy matching behavior
- changing cache lifetimes or import behavior
- migrating the release parser module

## risks

- **False positives.** An incomplete index key could link an incompatible
  episode. The key must contain every field checked by the current matcher.
- **False negatives.** Duplicate keys and renamed files can make a single-value
  index lose valid targets. The index must retain stable target order and the
  exact parsed-file fallback behavior where required.
- **Client overload.** Parallel per-hash reads can overload a remote client.
  Each adapter must own batching or bounded concurrency behind one interface.
- **Partial reads.** A bulk request can return some results and some failures.
  Planning must keep the current best-effort candidate behavior and preserve
  useful per-torrent errors.

## steps

1. Record current matching behavior and benchmark baselines. [done]
2. Add a parsed episode-file representation and indexed target matcher in
   `internal/release`. [done]
3. Integrate the index into exact import planning and preserve link ordering.
   [done]
4. Add regression tests and persistent benchmarks for 12, 24, and 100 episode
   plans. [done]
5. Audit primary client and library source for bulk file-detail capabilities.
   [done]
6. Select and implement the torrent-client file-read interface. [done]
7. Run focused formatting, tests, race checks, benchmarks, and documentation
   review. Move this plan to completed. [done]

## decision log

- 2026-08-11: keep indexed CPU matching and client read reduction in one PR.
  Both changes are local to exact plan construction and share the same
  behavior and performance verification surface.
- 2026-08-11: supersede the single-PR packaging decision. Ship client file-read
  batching first, then stack the parse-once indexed matcher on it. The
  torrent-client seam keeps the two implementations independently reviewable.
- 2026-08-11: place parsed episode matching in the release module. The HTTP
  processor should orchestrate candidates and links without knowing parser
  fields or normalization rules.
- 2026-08-11: keep client-specific batching and concurrency behind the
  torrent-client seam. The processor should request candidate file details once
  without selecting an adapter strategy.
- 2026-08-11: `GetFiles` returns one ordered `FileResult` per input hash.
  Per-hash errors preserve successful reads and the existing best-effort plan
  behavior.
- 2026-08-11: Transmission and Deluge v2 use one native multi-hash request.
  Deluge v1 first filters IDs through `SessionState` because an unknown ID
  causes the pinned wrapper to fail while decoding an empty status dictionary.
  qBittorrent uses four concurrent single-hash requests because the pinned Go
  wrapper does not expose the qBittorrent 5.2 bulk file response.

## verification notes

- Baseline synthetic benchmark on Apple M4 Pro, Go 1.26.5: current nested
  matching took about 12.7 ms for 12 episodes, 48.6 ms for 24 episodes, and
  823 ms for 100 episodes.
- A temporary parse-once map prototype took about 2.04 ms, 4.09 ms, and 17.0
  ms for the same cases. Temporary benchmark files were removed after the run.
- The checked-in matcher benchmark measures about 1.97 ms, 3.96 ms, and 16.8
  ms for 12, 24, and 100 episodes. The 100-episode case drops from about 72 MB
  and 1.05 million allocations to 3.5 MB and 24,000 allocations.
- `go test ./...` passed after indexed matching was integrated.
- `go test -race ./internal/release ./internal/http` passed.
- Primary-source client findings are recorded in
  `docs/references/torrent-client-file-read-performance.md`.
- Focused torrent-client, HTTP, and release tests pass after bulk file reads.
- `go test -race ./internal/release ./internal/http ./internal/torrentclient`
  passed.
- `gofumpt -w .`, `go test -v ./...`, `go vet ./...`, and
  `git diff --check` passed.
- Final checked-in benchmark medians: indexed matching takes about 1.89 ms for
  12 episodes, 3.79 ms for 24 episodes, and 15.9 ms for 100 episodes. The
  corresponding nested scan takes about 12.2 ms, 46.1 ms, and 778 ms.
- `govulncheck ./...` found no vulnerabilities in called code or imported
  packages. One required-module vulnerability is not called.
- Disposable live-client tests passed against qBittorrent 5.2.3, Transmission
  4.1.3, Deluge 1.3.15, and Deluge 2.1.2.dev0. Each adapter imported complete
  and partial torrents. The file-read probe requested 24 successful results and
  one missing result. Observed probe times were 22.5 ms, 1.7 ms, 2.7 ms, and
  1.4 ms respectively. This probe validates request shape and compatibility. It
  is not a sustained-throughput benchmark.
