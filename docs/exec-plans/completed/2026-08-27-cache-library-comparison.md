# Cache library comparison

## Goal

Identify the best low-memory cache design for seasonpackarr's import plans and
inventory snapshots. Compare correctness, request performance, retained heap,
and allocations against the current `xsync` implementation.

## Scope

- inspect `haxmap`, `pb`, and a small set of distinct Go cache designs
- test fixed TTL, immediate visibility, explicit invalidation, and concurrency
- benchmark representative plan-cache operations
- measure retained heap and object counts separately from shared plan payloads
- record a primary-source-cited recommendation

## Non-goals

- change production cache code or dependencies
- benchmark unrelated parsing, matching, or torrent-client work
- select a library only from upstream benchmark claims

## Risks

- cache APIs can have different consistency and expiry guarantees
- microbenchmarks can favor an unrealistic key count or hit ratio
- heap measurements can include runtime noise or shared payload memory
- async admission can make a fast `Set` comparison misleading

## Steps

1. Inspect required repositories and select distinct candidates. [complete]
2. Build a pinned external benchmark harness. [complete]
3. Run repeated operation, allocation, and retained-heap measurements. [complete]
4. Validate lifecycle and expiry behavior. [complete]
5. Write the comparison and decision. [complete]
6. Run documentation checks. [complete]
7. Add realistic 1,000, 10,000, and 50,000-torrent inventory benchmarks. [complete]
8. Measure cold build, warm refresh, cached access, title lookup, and retained heap. [complete]
9. Correct the recommendation to distinguish inventory snapshots from import plans. [complete]
10. Verify the expansion. [complete]
11. Cache comparable titles with parsed releases. [complete]
12. Add 0%, 1%, 10%, and 100% inventory churn benchmarks. [complete]
13. Compare refresh time, allocations, and retained heap against the baseline. [complete]
14. Evaluate shared release storage only if retained memory remains material. [complete]
15. Verify behavior, benchmarks, and documentation. [complete]
16. Add and compare 5,000-torrent cold, warm, and retained-memory cases. [complete]
17. Verify the expanded size matrix and complete the plan. [complete]

## Decision log

- 2026-08-27: evaluate map-only and full-cache libraries separately. A map-only
  replacement still needs seasonpackarr's TTL and cleanup policy.
- 2026-08-27: count shared torrent and link payloads before the heap baseline so
  retained-heap measurements isolate cache structure and copied metadata.
- 2026-08-27: exclude BigCache from the typed benchmark. A raw byte benchmark
  would hide the required key and plan serialization cost.
- 2026-08-27: keep xsync. Replace the full publish-time sweep separately if
  plan-cache growth becomes measurable. Use Otter only when a weighted bound is
  a product requirement.
- 2026-08-27: the first harness covers `planMap`, not `entryMap`. Inventory
  size is the number of torrents inside one per-client snapshot, not the outer
  xsync entry count. Benchmark the real snapshot construction path separately.
- 2026-08-27: use a gated inventory optimization. Cache comparable titles
  first. Pursue shared release storage only if the measured memory trade remains
  material after the simpler change.
- 2026-08-27: keep both inventory changes. At 50,000 unchanged torrents they
  reduce warm-refresh time by about 96.5%, cumulative bytes by 97.9%, and
  retained heap by 26.5%. Cold-build time rises about 3%.
- 2026-08-27: matched cold-build samples show no clear time regression from
  1,000 through 10,000 torrents. The 50,000 stress case remains 2.8% slower.
  Cold cumulative bytes fall about 11% at every measured size.

## Verification notes

- Pinned harness: `/Users/nuxen/dev/oss/seasonpackarr-cache-bench`.
- `go test ./...`: pass.
- `go test -race ./...`: pass.
- Eight 400 ms benchmark samples on Go 1.27.0, Apple M4 Pro, 12 logical CPUs.
- Seven isolated retained-memory samples per candidate at empty, 128, and 4,096
  entries.
- Five inventory benchmark samples at 1,000, 10,000, and 50,000 torrents.
- Seven isolated retained-memory samples per inventory size.
- `git diff --check`: pass.
- New-file whitespace checks: pass.
- Simplified English dash check: pass.
- `go test ./internal/http`: pass.
- `go test -race ./internal/http`: pass.
- `go test ./...`: pass after inventory optimization.
- Six-sample cold, warm, and churn benchmarks: pass.
- Seven isolated retained-memory samples at each inventory size: pass.
- Eight matched cold-build samples at 1,000, 5,000, and 10,000 torrents: pass.
- Six matched cold-build samples at 50,000 torrents: pass.
