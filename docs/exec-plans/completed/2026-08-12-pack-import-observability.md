# Pack and import observability

## goal

Make `/api/pack` explain every torrent episode that cannot be reused. Make
`/api/parse` explain where client-import time is spent.

## scope

- return compact unmatched-target diagnostics from exact episode matching
- preserve indexed matching performance and deterministic match selection
- log one structured event per unmatched torrent target
- return neutral attempted-stage timing from each torrent-client adapter,
  including the stage that failed
- log plan source, hardlink duration, client-import stages, and total parse time
- add matcher, adapter, and authenticated HTTP regression coverage
- update operator and design documentation

## non-goals

- changing episode compatibility or smart-mode acceptance
- restoring the old source-by-target mismatch log flood
- adding configuration or changing log levels
- reading or logging torrent-client credentials

## risks

- **False explanations.** Diagnostics must not claim one mismatch when a closer
  compatible source exists. Select the stable closest same-episode source and
  report every differing compatibility field.
- **Matching regressions.** Diagnostic work must not change the indexed lookup,
  target order, or first-source deduplication.
- **Interface spread.** Import timing must remain one neutral result behind the
  existing torrent-client seam, not a logger dependency in every adapter.
- **Timing ambiguity.** Stage duration is diagnostic data, not a performance
  guarantee. Skipped stages must be absent instead of reported as zero work.

## steps

1. Add a failing matcher-interface test for missing source, closest mismatch,
   and duplicate target diagnostics. [done]
2. Implement diagnostic matching without changing successful selection. [done]
3. Add failing adapter tests for successful import-stage reports. [done]
4. Implement neutral stage reports in qBittorrent, Transmission, and Deluge.
   [done]
5. Add failing authenticated endpoint log tests for pack reasons and parse
   stage timing. [done]
6. Implement structured processor logs and total timing. [done]
7. Update docs, run focused and full verification, then review the diff on
   standards and spec axes. [done]
8. Address review findings, complete the plan, and commit the work. [done]

## decision log

- 2026-08-12: use `EpisodeMatcher.Match` as the diagnostic seam. The release
  module owns compatibility knowledge; the HTTP module owns log policy.
- 2026-08-12: diagnose only unmatched targets after the indexed success pass.
  Do not restore the former quadratic mismatch logging.
- 2026-08-12: return a neutral import report from `TorrentClient.Import`.
  Adapters own stage measurement; the processor owns structured output.
- 2026-08-12: include failed-stage timing. Failure latency is operationally
  useful, and the existing `ImportError` already identifies the failed stage.
- 2026-08-12: index client sources by season and episode for diagnostics. This
  avoids restoring the old full source-target comparison matrix.
- 2026-08-12: retain a failed exact plan long enough to log every unmatched
  target before returning the existing failure status.
- 2026-08-12: use the implementation start commit `60f9b39` as the final review
  fixed point.

## verification notes

- Production baseline assertion on `hades`:
  `RED: pack-reason-missing parse-stage-timing-missing`.
- No production configuration, environment, or credential data was inspected.
- Focused release, torrent-client, and HTTP package tests pass.
- Indexed 100-episode benchmark after diagnostics: 16.8 ms median across three
  10-iteration runs. This is near the prior documented 15.9 ms result.
- `gofumpt` and `git diff --check` pass.
- `go test -v ./...` passes. Environment-gated live-client tests were skipped
  because their external test environments are not configured.
- `go test -race ./internal/release ./internal/http
  ./internal/torrentclient` passes.
- `go vet ./...` passes.
- `govulncheck ./...` reports no called-code or imported-package
  vulnerabilities. One required-module vulnerability is not called.
- Parallel standards and spec reviews found missing failure-path total timing,
  post-add qBittorrent hash validation, and skipped-stage coverage. All findings
  were fixed and regression-tested. The minor repeated-switch suggestion was
  rejected because the explicit five-case mappings are small and clearer than
  a metadata table.
