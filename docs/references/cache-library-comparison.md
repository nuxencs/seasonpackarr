# Cache library comparison

Research date: 2026-08-27

## Decision summary

Keep `xsync.MapOf` as the default. The local benchmark finds no lower-memory production-ready replacement. PB retains 2.3 KiB less heap at 128 entries, but nearly doubles the heap-object count, cannot store the plan value directly, and depends on runtime internals. The plan cache has a two-minute lifetime, rejects expired values during `Load`, deletes by exact key, and sweeps old entries before each store. A raw concurrent map keeps this policy visible and has no background lifecycle to manage. The current implementation and retained value shape are in [`processor_plan.go`](https://github.com/nuxencs/seasonpackarr/blob/2267dcb63542a7fa75c38b81024bb0569ee9902b/internal/http/processor_plan.go#L43-L58) and its [store and load functions](https://github.com/nuxencs/seasonpackarr/blob/2267dcb63542a7fa75c38b81024bb0569ee9902b/internal/http/processor_plan.go#L241-L278).

The actionable performance issue is the caller's full expiry sweep before every publish. The measured publish cost rises from 359 ns with one live plan to 2.48 us with 128. A later optimization should amortize or remove that sweep while preserving read-time expiry rejection. Changing the underlying concurrent map does not solve the policy cost.

The inventory cache has a different shape. `entryMap` has one outer xsync entry per torrent client. That entry owns a snapshot of every torrent returned by the client, indexed by comparable title and by torrent name. Before inventory optimization, a 50,000-torrent snapshot retained about 191.7 MiB. Caching the comparable title and sharing one immutable parsed release between both indexes reduces this to 140.9 MiB. An unchanged warm refresh falls from 134.2 ms and 608 MiB of cumulative allocation to 4.67 ms and 12.72 MiB. The outer xsync access remains about 62 ns. Replacing that one-entry-per-client map cannot materially reduce inventory cost.

If an application-level byte budget becomes a requirement, benchmark Otter v2.3.0 first. It accepts the existing comparable struct key, gives synchronous write visibility, has exact read-time expiry, supports fixed expiry after write, and has a custom weight limit. Its limit is eventual, so the cache can temporarily exceed it. The weigher must include the retained capacities of torrent bytes, slices, strings, and client configuration. Otter documents both the temporary excess and the static per-entry weight calculation in its [options](https://github.com/maypok86/otter/blob/a01a3c3138788cf34cf66b2bdcbe5cb0bac3bb32/options.go#L45-L105).

Jellydator TTLCache v3.4.1 is the simpler strict-accounting alternative. It applies count and custom cost limits synchronously. The custom cost is not a process-heap limit. Library metadata remains outside the caller's cost function. Its public `Get` takes the cache-wide exclusive lock because it updates the LRU list, even when TTL touch is disabled. Each entry also needs an item object, list element, and expiration-heap slot. See the [cache structure and constructor](https://github.com/jellydator/ttlcache/blob/7e8ce589256b0b6dc5bd757e84a7999df80eb59d/cache.go#L25-L87), [read path](https://github.com/jellydator/ttlcache/blob/7e8ce589256b0b6dc5bd757e84a7999df80eb59d/cache.go#L222-L297), and [capacity enforcement](https://github.com/jellydator/ttlcache/blob/7e8ce589256b0b6dc5bd757e84a7999df80eb59d/cache.go#L140-L219).

Do not select these candidates for the plan cache without a new reason:

- HaxMap cannot use `importPlanCacheKey`. Its key constraint only permits integer, floating-point, complex, string, `uintptr`, and unsafe-pointer types. It would require key encoding. Its source also warns that a concurrent resize can delay visibility of a set until the resize completes. Upstream has unresolved reports of [crashes during grow](https://github.com/alphadose/haxmap/issues/55), [delete hangs](https://github.com/alphadose/haxmap/issues/58), and [non-atomic GetOrSet and GetOrCompute behavior](https://github.com/alphadose/haxmap/issues/54). There has been no release or commit after v1.4.1. See its [key constraint](https://github.com/alphadose/haxmap/blob/fae115ca090791375c15c5c5188bba8428a08cf4/map.go#L31-L34) and [Set contract](https://github.com/alphadose/haxmap/blob/fae115ca090791375c15c5c5188bba8428a08cf4/map.go#L151-L183).
- Ristretto v2 restricts keys to primitive types and buffers new sets. A caller must encode the key and call `Wait` before it can depend on read-after-write visibility. A set can also be dropped or rejected. See the [key types](https://github.com/hypermodeinc/ristretto/blob/67cb59139a2f93fab17e7f199a5c5b3dbc7850db/z/z.go#L16-L18) and [set and wait paths](https://github.com/hypermodeinc/ristretto/blob/67cb59139a2f93fab17e7f199a5c5b3dbc7850db/cache.go#L267-L368).
- BigCache requires string keys and serialized `[]byte` values. A normal `Get` does not reject an expired entry. Correct use needs a codec and an expiry-aware wrapper around `GetWithInfo`. The serialization and copy costs work against the current typed, in-process value. See the [public key and value contract](https://github.com/allegro/bigcache/blob/ae1c781e48dc54fabe3a5b90bb98bc7e79553c8b/README.md#L3-L8), [Get methods](https://github.com/allegro/bigcache/blob/ae1c781e48dc54fabe3a5b90bb98bc7e79553c8b/bigcache.go#L135-L166), and [value copy](https://github.com/allegro/bigcache/blob/ae1c781e48dc54fabe3a5b90bb98bc7e79553c8b/encoding.go#L47-L54).
- Hashicorp's expirable LRU starts a TTL goroutine that v2.0.7 cannot stop. The source explicitly says that its done channel is never closed. See its [constructor and lifecycle note](https://github.com/hashicorp/golang-lru/blob/d8515860cebc7b25ff2d29fada3f10a43611c28b/expirable/expirable_lru.go#L47-L96).
- Theine v0.6.2 and Ristretto add admission-policy and maintenance structures intended for larger, contested caches. They are not a good default for a small, short-lived handoff cache. Theine also has no v2 release. Its official module remains `github.com/Yiling-J/theine-go` at [v0.6.2](https://github.com/Yiling-J/theine-go/releases/tag/v0.6.2).
- Autobrr go-cache PR 1 has correct fixed TTL when update-on-read is disabled, but it is unreleased, requires Go 1.27, has no capacity limit, and serializes operations through one cache-wide mutex. Keep it as a benchmark subject, not a dependency. See its [cache structure](https://github.com/autobrr/go-cache/blob/a7bfba68d289d7832ead6067bc299c5899935e5c/ttlcache/ttlcache.go#L22-L45), [fixed-TTL option](https://github.com/autobrr/go-cache/blob/a7bfba68d289d7832ead6067bc299c5899935e5c/ttlcache/ttlcache.go#L223-L242), and [module version](https://github.com/autobrr/go-cache/blob/a7bfba68d289d7832ead6067bc299c5899935e5c/go.mod#L1-L3).

## Requirement matrix

`Exact fixed TTL` means that reads do not extend the deadline and that an expired value is never returned. `Immediate` means that a successful set is visible to the next get without a flush or wait call.

| Candidate | Exact fixed TTL | Set to Get | Delete | Limit | Types | Lifecycle |
| --- | --- | --- | --- | --- | --- | --- |
| xsync v3.5.1 | Caller policy | Immediate | Immediate | None | `comparable`, `any` | No goroutine |
| HaxMap v1.4.1 | Caller policy | Immediate except documented concurrent-resize delay | Immediate logical delete | None | Primitive-like key, `any` | No goroutine |
| pb `MapOf` v1.5.25 | Caller policy | Immediate | Immediate | None | `comparable`, `any` | No permanent goroutine |
| autobrr PR 1 | Yes with `DisableUpdateTime(true)` | Immediate | Immediate | None | `comparable`, `any` | One goroutine, `Close` required |
| Jellydator TTLCache v3.4.1 | Yes with touch disabled | Immediate | Immediate | Strict count or custom cost | `comparable`, `any` | Optional blocking `Start`, then `Stop` |
| Hashicorp expirable v2.0.7 | Yes, after create/update | Immediate | Immediate | Strict entry count | `comparable`, `any` | TTL goroutine cannot stop |
| Theine v0.6.2 | Yes with `SetWithTTL` | Immediate | Immediate | Entry count or custom cost, maintenance follows mutation | `comparable`, `any` | Maintenance goroutines, `Close` |
| Ristretto v2.4.2 | Yes | Not for new entries without `Wait` | Immediate map delete | Eventual custom cost | Primitive key, `any` | One process goroutine, `Close` |
| Otter v2.3.0 | Yes with `ExpiryWriting` | Immediate | Immediate | Eventual count or custom weight | `comparable`, `any` | Expiry cleanup goroutine, automatic cleanup or explicit stop |
| BigCache v3.2.0 | Not through normal `Get` | Immediate | Immediate | Backing queue in MB, total memory is higher | `string`, serialized `[]byte` | Optional cleanup goroutine, `Close` |

The three raw maps do not replace the current expiry check and store-time sweep. This is an integration property, not a defect. They also do not replace the per-client refresh mutex used by the inventory cache. That mutex prevents duplicate upstream fetches, while the map only protects stored values. The inventory implementation shows the [map and cached mutex](https://github.com/nuxencs/seasonpackarr/blob/2267dcb63542a7fa75c38b81024bb0569ee9902b/internal/http/processor_candidate.go#L28-L46) and the [double-checked deadline logic](https://github.com/nuxencs/seasonpackarr/blob/2267dcb63542a7fa75c38b81024bb0569ee9902b/internal/http/processor_candidate.go#L191-L215).

## Memory and allocation design

No upstream result gives a fair memory comparison for the current value type and workload. The local benchmark below reports `B/op`, `allocs/op`, retained heap after a forced GC, fixed empty-cache cost, and retained heap at 128 and 4,096 entries. It separates the common plan payload from library metadata so that cache overhead stays visible.

| Candidate | Fixed and per-entry structures | Important allocation or retention behavior |
| --- | --- | --- |
| xsync | CLHT buckets and immutable entry nodes | No TTL, LRU, timer, or policy metadata. A good low-complexity baseline. The v3 design uses lock-free reads and bucket-level write locks ([source](https://github.com/puzpuzpuz/xsync/blob/800e3a0ceeab7d9a5c17df16241c1a4cca0da524/mapof.go#L21-L82)). |
| HaxMap | Lock-free ordered nodes, hash index, atomic pointers | New values and nodes are pointer-backed. Logical deletion and list maintenance can delay reclamation. Key encoding would add work and can allocate. See the [map fields and nodes](https://github.com/alphadose/haxmap/blob/fae115ca090791375c15c5c5188bba8428a08cf4/map.go#L37-L55). |
| pb `MapOf` | Bucket table, padded counter stripes, rebuild state | Optional shrinking can return table capacity after churn. Cache-line padding and preallocation can raise fixed cost for a small map. See its [map and table types](https://github.com/llxisdsh/pb/blob/cba48ae15494558b919f4ac9381e61f1f005b8a9/mapof.go#L113-L183). |
| autobrr PR 1 | One map, one item per entry, one timer goroutine | Small policy surface, but its expiry loop scans the full map when the timer fires ([source](https://github.com/autobrr/go-cache/blob/a7bfba68d289d7832ead6067bc299c5899935e5c/ttlcache/expiration.go#L10-L74)). |
| Jellydator TTLCache | Map, LRU list, expiry heap, metrics and event maps | Each value has an `Item` with a mutex, deadline, heap index, version, cost function, and cost. It also has a list element and heap reference ([source](https://github.com/jellydator/ttlcache/blob/7e8ce589256b0b6dc5bd757e84a7999df80eb59d/item.go#L28-L49)). |
| Hashicorp expirable | LRU list plus 100 expiration buckets and one mutex | The 100 maps and cleanup goroutine are fixed costs even for a small TTL cache. See the [type and constructor](https://github.com/hashicorp/golang-lru/blob/d8515860cebc7b25ff2d29fada3f10a43611c28b/expirable/expirable_lru.go#L16-L96). |
| Theine | Sharded map, admission sketch, eviction policy, read/write buffers, timer wheel | At least 16 shards. Entries carry policy and timer links plus atomic weight and deadline fields. This is useful under sustained contention, but it raises the small-cache floor. See [store construction](https://github.com/Yiling-J/theine-go/blob/1be01cecc132811bd12f79f895ceb10ba28e3a9b/internal/store.go#L161-L220) and [entry fields](https://github.com/Yiling-J/theine-go/blob/1be01cecc132811bd12f79f895ceb10ba28e3a9b/internal/entry.go#L35-L43). |
| Ristretto | 256 map shards, admission sketch, policy, access buffers, 32,768-slot set channel | The fixed set channel and shard count are material for a small cache. New `SetWithTTL` calls allocate an item for the channel ([source](https://github.com/hypermodeinc/ristretto/blob/67cb59139a2f93fab17e7f199a5c5b3dbc7850db/cache.go#L23-L25), [constructor](https://github.com/hypermodeinc/ristretto/blob/67cb59139a2f93fab17e7f199a5c5b3dbc7850db/cache.go#L204-L264)). |
| Otter | Feature-specific node layout, hash map, maintenance buffers, optional admission and expiration structures | It starts with a default initial capacity of 16 and only builds eviction or expiry policy when configured. This is a better small-cache shape than Ristretto, but still more metadata than a raw map. See [options](https://github.com/maypok86/otter/blob/a01a3c3138788cf34cf66b2bdcbe5cb0bac3bb32/options.go#L20-L105) and [construction](https://github.com/maypok86/otter/blob/a01a3c3138788cf34cf66b2bdcbe5cb0bac3bb32/cache_impl.go#L115-L202). |
| BigCache | Sharded key index plus byte queues | The caller must serialize. Set copies the byte result into the queue. Get decodes and copies value bytes. The hard byte limit applies to queues, while maps and statistics need more memory ([configuration](https://github.com/allegro/bigcache/blob/ae1c781e48dc54fabe3a5b90bb98bc7e79553c8b/config.go#L25-L31)). Its main memory advantage targets very large entry counts, not rich Go object graphs. |

## Local import-plan benchmark results

The benchmark used Go 1.27.0 on an Apple M4 Pro with 24 GiB of memory and macOS 26.6.2. Each latency result is the median of eight 400 ms samples with 12 logical CPUs. The representative value has 64 KiB of torrent bytes, 24 planned links, unmatched entries, hashes, release fields, client configuration, and a deadline. The key has the client name plus v1 and v2 torrent hashes.

The raw-map adapters preserve seasonpackarr's current read-time expiry check and full expired-entry sweep before every publish. Ristretto calls `Wait` after every set because the next get must see the plan. HaxMap and Ristretto encode the struct key as a string. The PB benchmark uses `*cachedImportPlan`: `pb.MapOf[importPlanCacheKey, cachedImportPlan]` fails to compile because the generated `nosplit` stack exceeds Go's 792-byte limit with this value shape.

### Request cost

`Parallel hit` is the cost per lookup across the benchmark's 12 logical CPUs. `Publish 128` updates one plan while 128 plans are live. Allocation columns use that same update path.

| Candidate | Hit ns/op | Hit B/op | Hit allocs | Parallel hit ns/op | Publish 128 ns/op | Publish B/op | Publish allocs | Delete and restore ns/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| xsync current | 117.2 | 0 | 0 | 12.61 | 2,480 | 512 | 1 | 415.2 |
| PB pointer value | 78.87 | 0 | 0 | 8.90 | 537.9 | 512 | 2 | 191.7 |
| HaxMap encoded key | 130.3 | 128 | 1 | 36.12 | 1,872 | 576 | 2 | 249.7 |
| autobrr PR 1 | 128.7 | 0 | 0 | 139.1 | 211.3 | 0 | 0 | 327.2 |
| Jellydator TTLCache | 113.7 | 0 | 0 | 359.3 | 248.2 | 0 | 0 | 421.4 |
| Hashicorp expirable | 107.9 | 0 | 0 | 292.1 | 181.4 | 0 | 0 | 293.3 |
| Theine | 185.6 | 16 | 1 | 19.04 | 292.2 | 0 | 0 | 569.0 |
| Ristretto, encoded key and `Wait` | 303.8 | 136 | 1 | 52.16 | 916.8 | 1,264 | 4 | 1,464 |
| Otter | 288.8 | 0 | 0 | 23.51 | 604.2 | 1,034 | 2 | 719.6 |

The current full sweep is the important latency problem. Its publish time rises from 359 ns with one live entry to 2.48 us with 128. Autobrr PR 1 and the direct TTL caches stay near constant time. However, PR 1, Jellydator, and Hashicorp serialize parallel reads enough to lose the present map's scaling. PB is fast, but its pointer value adds an object per entry and its runtime-internal dependency is not an acceptable maintenance trade.

### Retained heap and construction allocations

The retained test creates keys and representative plans before the baseline, fills the cache, forces a GC, and keeps the cache live. The 128-entry values are medians from seven isolated processes with eight cache instances per process. The 4,096-entry values are medians from seven isolated processes with one cache instance. `Build bytes` is cumulative allocation while an empty cache becomes a 128-entry cache. Empty-cache values can include lazy initialization and should be read as small-cache floor estimates, not exact object sizes.

| Candidate | Empty heap | Heap at 128 | Objects at 128 | Build bytes at 128 | Heap at 4,096 | Objects at 4,096 | Extra goroutines |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| xsync current | 2.6 KiB | 68.6 KiB | 138 | 71.5 KiB | 2.13 MiB | 4,141 | 0 |
| PB pointer value | 2.1 KiB | 66.3 KiB | 264 | 66.8 KiB | 2.07 MiB | 8,315 | 0 |
| HaxMap encoded key | 0.2 KiB | 81.0 KiB | 391 | 83.8 KiB | 2.51 MiB | 12,303 | 0 |
| autobrr PR 1 | 0.9 KiB | 78.9 KiB | 142 | 97.6 KiB | 2.44 MiB | 4,126 | 1 |
| Jellydator TTLCache | 2.7 KiB | 109.3 KiB | 404 | 128.9 KiB | 3.37 MiB | 12,336 | 1 |
| Hashicorp expirable | 10.0 KiB | 118.1 KiB | 246 | 154.7 KiB | 3.39 MiB | 4,250 | 1, cannot stop in v2.0.7 |
| Theine | 288.1 KiB | 379.8 KiB | 823 | 382.4 KiB | 3.11 MiB | 4,862 | 2 |
| Ristretto, encoded key and `Wait` | 285.7 KiB | 383.1 KiB | 796 | 560.5 KiB | 2.94 MiB | 5,455 | 2 |
| Otter | 100.8 KiB | 190.5 KiB | 478 | 194.8 KiB | 2.59 MiB | 4,620 | 1 |

These numbers exclude the shared backing arrays and strings in the representative plan. This isolates the cache implementation, but it is not the service's total cache memory. If all 128 plans own a distinct 64 KiB torrent buffer, those buffers alone add 8 MiB for every typed cache. A byte or weight limit must count that retained capacity. PB saves only 2.3 KiB against xsync at 128 entries while nearly doubling the heap-object count. The policy caches have much larger small-cache floors. Otter costs about 122 KiB more than xsync at 128 entries, which is the price of expiration and bounded weighted eviction.

BigCache is not in the common benchmark. It needs a key codec and a full plan serializer, and no such production codec exists. A raw `[]byte` benchmark would omit the cost that determines whether BigCache fits this code. Its source-level result remains a rejection for this object cache.

## Local inventory benchmark results

The inventory benchmark runs the real `getAllTorrents` path and real `rls.ParseString` and `format.ComparableTitle` calls. Its synthetic client has 20 episodes per comparable title, unique 40-character hashes, realistic release names, and distinct save paths. The benchmark covers 1,000, 5,000, 10,000, and 50,000 torrents. The code is in `internal/http/processor_inventory_benchmark_test.go`.

### Baseline

`Cold build` starts without a prior snapshot, so it parses every release. `Warm refresh` expires an existing snapshot, reuses `rlsMap`, and rebuilds both indexes. `Cached access` calls `getAllTorrents` while the 30-second snapshot is valid. Results are medians from five 300 ms benchmark samples on the same Go 1.27.0 and Apple M4 Pro environment as the import-plan benchmark.

| Torrents | Cold build | Cold B/op | Cold allocs | Warm refresh | Warm B/op | Warm allocs |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1,000 | 110.1 ms | 19.34 MiB | 135,400 | 2.607 ms | 12.18 MiB | 18,330 |
| 5,000 | 556.3 ms | 95.50 MiB | 676,200 | 11.93 ms | 60.85 MiB | 91,560 |
| 10,000 | 1.134 s | 190.0 MiB | 1,351,000 | 24.60 ms | 121.7 MiB | 183,100 |
| 50,000 | 5.616 s | 944.7 MiB | 6,747,000 | 134.2 ms | 608.0 MiB | 915,200 |

The retained-memory test constructs the source inventory after the baseline, builds the real snapshot, releases the source slice, forces a GC, and keeps the snapshot live. Therefore, it includes strings retained from the client result and both snapshot indexes. Each value is the median of seven isolated processes, except the 50,000-torrent value, which uses seven stable isolated samples split across two runs.

| Torrents | Title buckets | Retained heap | Heap objects | Retained bytes per torrent |
| ---: | ---: | ---: | ---: | ---: |
| 1,000 | 50 | 4.15 MiB | 37,529 | 4,352 |
| 5,000 | 250 | 19.41 MiB | 185,753 | 4,071 |
| 10,000 | 500 | 38.78 MiB | 371,483 | 4,066 |
| 50,000 | 2,500 | 191.7 MiB | 1,855,556 | 4,020 |

The baseline establishes that the inventory cache is not small, but the outer concurrent map is. Cached access and title lookup are constant and negligible across the measured sizes. A cold build costs about 110 to 113 microseconds and 135 allocations per torrent. Reusing parsed releases makes refresh about 42 times faster at 50,000 torrents, but rebuilding and normalizing the title index still allocates about 12.2 KiB per torrent cumulatively.

### Optimized snapshot

The optimized snapshot stores the comparable title with the parsed release. An unchanged refresh no longer normalizes every title. Both indexes also point to one immutable parsed release instead of retaining two large `rls.Release` values. Snapshot publication, the 30-second TTL, configuration scoping, and the per-client refresh mutex remain unchanged.

| Torrents | Warm refresh | Warm B/op | Warm allocs | Retained heap | Change from baseline heap |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 1,000 | 81.67 us | 293.0 KiB | 318 | 3.05 MiB | -26.5% |
| 5,000 | 386.3 us | 1.352 MiB | 1,536 | 14.31 MiB | -26.3% |
| 10,000 | 792.0 us | 2.708 MiB | 3,054 | 28.66 MiB | -26.1% |
| 50,000 | 4.669 ms | 12.72 MiB | 15,160 | 140.9 MiB | -26.5% |

At 50,000 unchanged torrents, the combined change reduces warm-refresh time by about 96.5%, cumulative bytes by 97.9%, allocation count by 98.3%, and retained heap by 26.5%. Heap-object count rises about 2.6% because each shared release and cached comparable title has its own retained object. Total retained bytes remain substantially lower.

Cold parsing remains the limiting case. Matched original and optimized runs use eight samples through 10,000 torrents and six samples at 50,000:

| Torrents | Original cold build | Optimized cold build | Time change | Original B/op | Optimized B/op | Byte change |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1,000 | 110.1 ms | 111.0 ms | +0.8% | 19.34 MiB | 17.30 MiB | -10.5% |
| 5,000 | 556.3 ms | 562.5 ms | +1.1% | 95.50 MiB | 84.89 MiB | -11.1% |
| 10,000 | 1.134 s | 1.135 s | +0.1% | 190.0 MiB | 169.0 MiB | -11.1% |
| 50,000 | 5.616 s | 5.775 s | +2.8% | 944.7 MiB | 840.0 MiB | -11.1% |

The 1,000 through 10,000 confidence intervals overlap, so there is no clear cold-time regression at realistic sizes. The 50,000 stress case has a measurable 2.8% slowdown, likely from garbage-collector scanning of the additional pointer objects. Cold cumulative bytes fall about 11% at every size. Cold parsing occurs for the first snapshot or changed release names, while unchanged refreshes are the common cache path.

The 10,000-torrent churn benchmark alternates two inventories so every changed name is a real parse miss:

| Changed names | Refresh time | B/op | Allocs |
| ---: | ---: | ---: | ---: |
| 0% | 802.4 us | 2.708 MiB | 3,054 |
| 1% | 12.47 ms | 4.382 MiB | 16,500 |
| 10% | 116.2 ms | 19.33 MiB | 137,600 |
| 100% | 1.170 s | 168.3 MiB | 1,350,000 |

The remaining cost scales with changed release names, not total cached inventory. A renamed release must be parsed again because `rlsMap` is keyed by the full name. Further optimization should target cold parsing only if production profiles show large churn or unacceptable startup latency. A different outer cache library remains out of scope.

## Candidate notes

### Raw concurrent maps

#### xsync v3.5.1

This is the current baseline. It accepts `importPlanCacheKey` without conversion. Loads are lock-free, writes lock a bucket, and `Store`, `Load`, and `Delete` have synchronous map semantics. It has no capacity, TTL, timer, or shutdown API. The current caller policy stays unchanged. The evaluated module requires Go 1.18 and uses Apache-2.0 at this revision ([go.mod](https://github.com/puzpuzpuz/xsync/blob/800e3a0ceeab7d9a5c17df16241c1a4cca0da524/go.mod#L1-L3), [license](https://github.com/puzpuzpuz/xsync/blob/800e3a0ceeab7d9a5c17df16241c1a4cca0da524/LICENSE)). It is mature and remains active, although current upstream development is on v4.

#### HaxMap v1.4.1

HaxMap is not a TTL cache. It uses a lock-free Harris linked list plus a hash index. `Get` reads the atomic value pointer. `Del` marks nodes and removes index entries. It has no count or byte limit and no goroutine. The primitive-only key constraint is a direct integration blocker for the current struct key. The resize visibility warning weakens the otherwise synchronous-looking API for this handoff use. It requires Go 1.18 and uses MIT ([go.mod](https://github.com/alphadose/haxmap/blob/fae115ca090791375c15c5c5188bba8428a08cf4/go.mod#L1-L3), [license](https://github.com/alphadose/haxmap/blob/fae115ca090791375c15c5c5188bba8428a08cf4/LICENSE)). The selected release is from 2024, and repository activity is lower than xsync.

#### pb `MapOf` v1.5.25

`MapOf[K comparable, V any]` is the closest raw-map alternative to xsync. It supports the current key, lock-free reads, bucket-level write locking, custom hashing, preallocation, and optional shrinking. `Load`, `Store`, and `Delete` are synchronous ([Load](https://github.com/llxisdsh/pb/blob/cba48ae15494558b919f4ac9381e61f1f005b8a9/mapof.go#L647-L679), [Store and Delete](https://github.com/llxisdsh/pb/blob/cba48ae15494558b919f4ac9381e61f1f005b8a9/mapof.go#L1442-L1553)). It has no TTL, capacity, or permanent goroutine. Large resize finalization can use a temporary goroutine. It requires Go 1.22 in its module file and uses MIT ([go.mod](https://github.com/llxisdsh/pb/blob/cba48ae15494558b919f4ac9381e61f1f005b8a9/go.mod#L1-L3), [license](https://github.com/llxisdsh/pb/blob/cba48ae15494558b919f4ac9381e61f1f005b8a9/LICENSE)). Its current `x/sys` dependency requires Go 1.24 in practice ([dependency go.mod](https://github.com/golang/sys/blob/v0.41.0/go.mod#L1-L3)). It also links to runtime spin functions and duplicates internal Go map layout to obtain runtime hash functions. The source warns that this layout must be checked after Go upgrades ([runtime integration](https://github.com/llxisdsh/pb/blob/cba48ae15494558b919f4ac9381e61f1f005b8a9/mapof.go#L3064-L3152), [map layout](https://github.com/llxisdsh/pb/blob/cba48ae15494558b919f4ac9381e61f1f005b8a9/mapof.go#L3267-L3346)). It is young, first published in 2025, with much less adoption than xsync. Treat it as an experimental benchmark candidate, not the default.

### Direct TTL caches

#### autobrr go-cache PR 1

PR 1 uses one `RWMutex`, a map of item pointers, one timer, and one expiration goroutine. `Get` checks the deadline before returning a value. With update-on-read disabled, reads do not move the deadline. Set and delete mutate the map before return ([read path](https://github.com/autobrr/go-cache/blob/a7bfba68d289d7832ead6067bc299c5899935e5c/ttlcache/internal.go#L14-L28), [mutation paths](https://github.com/autobrr/go-cache/blob/a7bfba68d289d7832ead6067bc299c5899935e5c/ttlcache/internal.go#L119-L223)). `Close` is required to stop the goroutine. It has no bound. It is MIT, but it has no released tag and PR 1 remains the integration surface.

#### Jellydator TTLCache v3.4.1

Configure `WithTTL(2*time.Minute)` and `WithDisableTouchOnHit()` for the present semantics. Expired values fail the read check even if `Start` is not running. `Start` provides eager heap-driven cleanup and blocks until `Stop`; construction does not start it automatically ([Start and Stop](https://github.com/jellydator/ttlcache/blob/7e8ce589256b0b6dc5bd757e84a7999df80eb59d/cache.go#L662-L749)). `WithCapacity` limits entries. `WithMaxCost` uses a caller cost function and evicts synchronously until cost is within the bound. The library requires Go 1.25 and uses MIT ([go.mod](https://github.com/jellydator/ttlcache/blob/7e8ce589256b0b6dc5bd757e84a7999df80eb59d/go.mod#L1-L3), [license](https://github.com/jellydator/ttlcache/blob/7e8ce589256b0b6dc5bd757e84a7999df80eb59d/LICENSE)). It is a long-lived project with an active v3 release line.

#### Hashicorp expirable LRU v2.0.7

The expirable cache uses one mutex, exact read-time rejection, fixed expiry after add/update, LRU count eviction, and immediate remove. Reads update LRU recency but do not reset the TTL ([Add and Get](https://github.com/hashicorp/golang-lru/blob/d8515860cebc7b25ff2d29fada3f10a43611c28b/expirable/expirable_lru.go#L117-L162)). `Contains` and `Len` can still count expired entries until cleanup. Size zero means no entry limit. The unclosable goroutine is the main blocker for service and test lifecycle. It requires Go 1.18 and uses MPL-2.0 ([go.mod](https://github.com/hashicorp/golang-lru/blob/d8515860cebc7b25ff2d29fada3f10a43611c28b/go.mod#L1-L3), [license](https://github.com/hashicorp/golang-lru/blob/d8515860cebc7b25ff2d29fada3f10a43611c28b/LICENSE)). The project is mature, but this selected v2 release is from 2023.

### Admission and weighted caches

#### Theine v0.6.2

Theine uses W-TinyLFU admission, sharded storage, a hierarchical timer wheel, and asynchronous maintenance. `SetWithTTL` writes the shard before it queues policy work, so the value is immediately readable. The read path rejects expired nodes. Delete removes from the shard before policy cleanup ([Get](https://github.com/Yiling-J/theine-go/blob/1be01cecc132811bd12f79f895ceb10ba28e3a9b/internal/store.go#L246-L305), [Set and Delete](https://github.com/Yiling-J/theine-go/blob/1be01cecc132811bd12f79f895ceb10ba28e3a9b/internal/store.go#L383-L501)). It supports entry count or custom cost. `Close` stops its maintenance context ([lifecycle](https://github.com/Yiling-J/theine-go/blob/1be01cecc132811bd12f79f895ceb10ba28e3a9b/internal/store.go#L812-L828)). It requires Go 1.20 and uses MIT ([go.mod](https://github.com/Yiling-J/theine-go/blob/1be01cecc132811bd12f79f895ceb10ba28e3a9b/go.mod#L1-L3), [license](https://github.com/Yiling-J/theine-go/blob/1be01cecc132811bd12f79f895ceb10ba28e3a9b/LICENSE)). It is promising but still pre-1.0.

#### Ristretto v2.4.2

Ristretto uses TinyLFU admission and sampled LFU eviction. Its maximum cost is enforced through buffered policy processing. `SetWithTTL` can return false when the set buffer is full, and an accepted new item can later lose admission. `Wait` provides a flush boundary. The read path rejects expired values ([configuration](https://github.com/hypermodeinc/ristretto/blob/67cb59139a2f93fab17e7f199a5c5b3dbc7850db/cache.go#L79-L183), [store expiry check](https://github.com/hypermodeinc/ristretto/blob/67cb59139a2f93fab17e7f199a5c5b3dbc7850db/store.go#L166-L181)). Delete is immediate in the store, with policy cleanup buffered. `Close` stops the process goroutine. It requires Go 1.24, declares a Go 1.25 toolchain, and uses Apache-2.0 ([go.mod](https://github.com/hypermodeinc/ristretto/blob/67cb59139a2f93fab17e7f199a5c5b3dbc7850db/go.mod#L1-L5), [license](https://github.com/hypermodeinc/ristretto/blob/67cb59139a2f93fab17e7f199a5c5b3dbc7850db/LICENSE)). It is the most widely adopted admission cache in this set, but its semantics and fixed structures do not fit this small handoff cache.

#### Otter v2.3.0

Use `ExpiryWriting[importPlanCacheKey, cachedImportPlan](2*time.Minute)` for fixed TTL. `GetIfPresent` checks the node deadline before return. `Set` updates the hash map synchronously, then records maintenance. `Invalidate` removes through the hash map compute operation ([read](https://github.com/maypok86/otter/blob/a01a3c3138788cf34cf66b2bdcbe5cb0bac3bb32/cache_impl.go#L246-L279), [set](https://github.com/maypok86/otter/blob/a01a3c3138788cf34cf66b2bdcbe5cb0bac3bb32/cache_impl.go#L429-L481), [invalidate](https://github.com/maypok86/otter/blob/a01a3c3138788cf34cf66b2bdcbe5cb0bac3bb32/cache_impl.go#L1188-L1202)). Expiration enables a one-second periodic cleanup goroutine. `runtime.AddCleanup` stops it when the public cache becomes unreachable; `StopAllGoroutines` is also available ([constructor](https://github.com/maypok86/otter/blob/a01a3c3138788cf34cf66b2bdcbe5cb0bac3bb32/cache.go#L80-L97), [stop API](https://github.com/maypok86/otter/blob/a01a3c3138788cf34cf66b2bdcbe5cb0bac3bb32/cache.go#L468-L478)). It requires Go 1.24 and uses Apache-2.0 ([go.mod](https://github.com/maypok86/otter/blob/a01a3c3138788cf34cf66b2bdcbe5cb0bac3bb32/go.mod#L1-L3), [license](https://github.com/maypok86/otter/blob/a01a3c3138788cf34cf66b2bdcbe5cb0bac3bb32/LICENSE)). It is active and has a stable v2 API.

### Serialized byte cache

#### BigCache v3.2.0

BigCache uses sharded maps from hashed string keys to offsets in byte queues. `LifeWindow` is global and timestamps use second resolution. A cleanup goroutine runs only when `CleanWindow` is positive. `HardMaxCacheSize` caps queue allocation in MB and evicts old entries, but it does not cap all cache memory ([configuration](https://github.com/allegro/bigcache/blob/ae1c781e48dc54fabe3a5b90bb98bc7e79553c8b/config.go#L13-L31)). The normal `Get` path returns queue data without an expiration rejection. `GetWithInfo` reports `EntryStatus`, so an adapter could turn expired status into a miss. That adapter still must encode the struct key and serialize every `cachedImportPlan`. It requires Go 1.22 and uses Apache-2.0 ([go.mod](https://github.com/allegro/bigcache/blob/ae1c781e48dc54fabe3a5b90bb98bc7e79553c8b/go.mod#L1-L3), [license](https://github.com/allegro/bigcache/blob/ae1c781e48dc54fabe3a5b90bb98bc7e79553c8b/LICENSE)). It is mature and useful for very large byte caches, but not for this object cache.

## Upstream benchmark quality

Do not use the upstream numbers to rank these candidates for seasonpackarr:

- xsync's checked-in benchmark report identifies v2.3.1, not the evaluated v3.5.1 ([report header](https://github.com/puzpuzpuz/xsync/blob/800e3a0ceeab7d9a5c17df16241c1a4cca0da524/BENCHMARKS.md#L1-L15)).
- HaxMap's README reports its own mixed workloads and machine, not TTL behavior or this value type. Its mixed read/write allocation count covers an outer benchmark operation that loops over 4,096 map operations, so it is not an allocation-per-map-operation value ([table](https://github.com/alphadose/haxmap/blob/fae115ca090791375c15c5c5188bba8428a08cf4/README.md#L66-L98), [benchmark source](https://github.com/alphadose/haxmap/blob/fae115ca090791375c15c5c5188bba8428a08cf4/benchmarks/map_test.go#L13-L88)).
- pb reports extremely small parallel times on a Windows 32-core machine. Parallel writers reuse the same local key sequence, and equal-value stores have a no-write fast path after warm-up. This does not measure cold insertion allocation. Its memory graph uses integer entries and provides no retained raw comparison data or chart generator ([performance table](https://github.com/llxisdsh/pb/blob/cba48ae15494558b919f4ac9381e61f1f005b8a9/README.md#L152-L177), [store fast path](https://github.com/llxisdsh/pb/blob/cba48ae15494558b919f4ac9381e61f1f005b8a9/mapof.go#L1442-L1462), [memory example](https://github.com/llxisdsh/pb/blob/cba48ae15494558b919f4ac9381e61f1f005b8a9/README.md#L324-L356)).
- Jellydator's benchmark package measures only `Set` and does not call `ReportAllocs` ([source](https://github.com/jellydator/ttlcache/blob/7e8ce589256b0b6dc5bd757e84a7999df80eb59d/bench/bench_test.go#L11-L26)).
- Theine's README points to a cross-library harness, but that retained harness uses older candidate versions. It is useful project evidence, not a current version comparison ([README](https://github.com/Yiling-J/theine-go/blob/1be01cecc132811bd12f79f895ceb10ba28e3a9b/README.md#L208-L290)).
- Ristretto publishes benchmark graphs, but they do not establish fixed cost, retained bytes for `cachedImportPlan`, or synchronous handoff behavior.
- Otter publishes a memory harness based on `runtime.MemStats.Alloc` deltas. Its post-population GC is disabled and its dependency set does not match all versions in this survey ([harness](https://github.com/maypok86/otter/blob/a01a3c3138788cf34cf66b2bdcbe5cb0bac3bb32/benchmarks/memory/main.go#L38-L95)).
- BigCache's README numbers use Go 1.13 and tens of millions of entries, which tests its intended scale but not this cache ([report](https://github.com/allegro/bigcache/blob/ae1c781e48dc54fabe3a5b90bb98bc7e79553c8b/README.md#L100-L133)).

Use one local harness and one Go toolchain for all candidates. Report latency and memory together. A faster hit is not a win if the empty cache or one-entry retained heap is much larger.

## Evaluated revisions

| Project | Revision | Go | License | Maturity signal |
| --- | --- | --- | --- | --- |
| puzpuzpuz/xsync | v3.5.1, `800e3a0` | 1.18 | Apache-2.0 | Mature, active; v4 now exists |
| alphadose/haxmap | v1.4.1, `fae115c` | 1.18 | MIT | Released 2024; lower recent activity |
| llxisdsh/pb | v1.5.25, `cba48ae` | 1.22 declared, 1.24 effective dependency floor | MIT | Young project, frequent releases, low adoption |
| autobrr/go-cache | PR 1, `a7bfba6` | 1.27 | MIT | Unreleased pull request |
| jellydator/ttlcache | v3.4.1, `7e8ce58` | 1.25 | MIT | Long-lived, active v3 |
| hashicorp/golang-lru | v2.0.7, `d851586` | 1.18 | MPL-2.0 | Mature project; selected release from 2023 |
| Yiling-J/theine-go | v0.6.2, `1be01ce` | 1.20 | MIT | Active pre-1.0 line; no v2 module |
| hypermodeinc/ristretto | v2.4.2, `67cb591` | 1.24, toolchain 1.25 | Apache-2.0 | Mature, active v2 |
| maypok86/otter | v2.3.0, `a01a3c3` | 1.24 | Apache-2.0 | Active, stable v2 API |
| allegro/bigcache | v3.2.0, `ae1c781` | 1.22 | Apache-2.0 | Mature, active v3 |
