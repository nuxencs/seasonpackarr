# xsync import-plan cache key audit

Date: 2026-08-09

## Question

Does changing the import-plan cache key from a formatted string to `importPlanCacheKey` cause a performance regression in xsync?

## Conclusion

The change has measurable effects in both directions. The typed key makes an isolated xsync operation slower because xsync must hash and compare several fields. However, the old implementation had to format and allocate a new combined string before each cache operation. A representative end-to-end benchmark shows that the typed key is faster and allocates less memory when key construction is included.

This cache performs one operation per webhook stage and keeps entries for two minutes. The measured nanosecond-level differences do not have a material effect on endpoint execution time. Client requests, torrent decoding, and filesystem work are much larger costs.

Keep the typed key. Do not describe it as having no performance impact. The accurate statement is: pure xsync operations are slower, but removal of string formatting makes the complete cache operation faster and reduces allocations.

## Exact implementation

seasonpackarr pins `github.com/puzpuzpuz/xsync/v3` at version `v3.5.1` in [`go.mod`](../../go.mod). The audited tag is commit `800e3a0ceeab7d9a5c17df16241c1a4cca0da524`.

`xsync.NewMapOf` installs its default hasher. `Load` invokes that hasher and then compares the complete key with `==`. See the [v3.5.1 MapOf constructor and lookup](https://github.com/puzpuzpuz/xsync/blob/v3.5.1/mapof.go#L84-L88) and [lookup implementation](https://github.com/puzpuzpuz/xsync/blob/v3.5.1/mapof.go#L167-L186).

For a non-interface comparable key, xsync calls Go's `runtime.typehash` on the complete key. See the [v3.5.1 default hasher](https://github.com/puzpuzpuz/xsync/blob/v3.5.1/util_hash.go#L36-L54). The Go runtime hashes a string with one string hash. It hashes a struct by hashing each non-blank field in order. See the [Go runtime type hash](https://github.com/golang/go/blob/go1.26.5/src/runtime/alg.go#L215-L258).

The current key contains three strings and one Boolean value across two nested structures:

```go
type importPlanCacheKey struct {
    clientName string
    hashes     torrents.Hashes
}

type Hashes struct {
    Legacy string
    V2     string
    HasV1  bool
}
```

As a result:

- A prebuilt string key needs one string hash and one string comparison.
- The typed key needs three string hashes, one Boolean hash, and field-by-field equality.
- The typed value is 56 bytes on the tested 64-bit platform. A string header is 16 bytes, but the formatted string also needs a separate backing allocation for its content.

xsync documents that built-in key hashing can be a significant part of a map benchmark. See the [v3.5.1 hashing guidance](https://github.com/puzpuzpuz/xsync/blob/v3.5.1/README.md#L90-L101).

## Representative benchmark

Environment:

- Apple M4 Pro, arm64
- macOS
- Go 1.26.5
- xsync v3.5.1
- Client name `default`
- 40-character legacy hash
- 64-character v2 hash
- Five measured runs, one second or more per run

The benchmark compared the previous key construction:

```go
fmt.Sprintf("%s|%t|%s|%s", clientName, hashes.HasV1, hashes.Legacy, hashes.V2)
```

with construction of the current typed key. Median results:

| Operation | Formatted string | Typed key | Result |
| --- | ---: | ---: | --- |
| Construct key | 87.5 ns, 160 B, 3 allocations | 2.62 ns, 0 B, 0 allocations | Typed key is cheaper |
| Load with prebuilt key | 7.39 ns, 0 allocations | 31.69 ns, 0 allocations | String is about 4.3 times faster |
| Construct and load | 98.7 ns, 160 B, 3 allocations | 31.55 ns, 0 B, 0 allocations | Typed path is about 3.1 times faster |
| Store with prebuilt key | 23.65 ns, 24 B, 1 allocation | 58.26 ns, 64 B, 1 allocation | String is about 2.5 times faster |
| Construct and store | 118.1 ns, 184 B, 4 allocations | 59.07 ns, 64 B, 1 allocation | Typed path is about 2 times faster |

The isolated xsync numbers show a real regression in hashing and entry-copy cost. The complete-operation numbers show that removal of `fmt.Sprintf` more than offsets that regression for the key shape used by seasonpackarr.

These figures are microbenchmarks, not a latency guarantee. Hardware, Go releases, hash contents, and contention can change the exact values. They are sufficient to reject both extreme claims: the typed key is not free, and it is not an end-to-end performance regression in the tested use case.

## Sources

- [`go.mod`](../../go.mod): pinned xsync version and Go language version
- [`internal/http/processor_plan.go`](../../internal/http/processor_plan.go): current key type and cache operations
- [`internal/torrents/torrents.go`](../../internal/torrents/torrents.go): hash key fields
- [xsync v3.5.1 `mapof.go`](https://github.com/puzpuzpuz/xsync/blob/v3.5.1/mapof.go)
- [xsync v3.5.1 `util_hash.go`](https://github.com/puzpuzpuz/xsync/blob/v3.5.1/util_hash.go)
- [xsync v3.5.1 README](https://github.com/puzpuzpuz/xsync/blob/v3.5.1/README.md)
- [Go 1.26.5 `runtime/alg.go`](https://github.com/golang/go/blob/go1.26.5/src/runtime/alg.go)
