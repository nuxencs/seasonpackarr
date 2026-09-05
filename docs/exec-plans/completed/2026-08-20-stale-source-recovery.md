# Stale hardlink source recovery

## goal

Show which client releases pass the candidate gate. Recover when a client
moves a planned episode before `/api/parse` creates its hardlink.

## scope

- debug-log each client release that passes `/api/candidate`
- detect a missing planned source during hardlinking
- refresh the client inventory and exact plan once, then retry hardlinking
- add authenticated HTTP regression coverage
- update lifecycle and operator documentation

## non-goals

- prevent Sonarr or a torrent client from moving files
- retry non-path hardlink errors
- add configuration
- remove hardlinks created before an aborted import

## risks

- repeated retries could hide an unstable client, so recovery is bounded to one
  refresh
- a refreshed plan can have lower coverage, so it must pass the planned and
  achieved coverage checks before import
- successful links from the first attempt remain on disk and must be accepted
  as idempotent successes during the retry

## steps

1. Capture the production symptom and add failing component tests. [done]
2. Prove whether plan-only or plan-plus-inventory refresh is required. [done]
3. Implement candidate debug logs and bounded stale-source recovery. [done]
4. Update docs and run focused and full verification. [done]
5. Review the final diff and complete this plan. [done]
6. Address independent review findings for cache and coverage helpers,
   structured missing-source logs, cause-neutral test naming, and explicit
   repeated-move coverage. [done]

## decision log

- 2026-08-20: Hades logs confirm an accepted plan referenced a source path that
  disappeared before hardlinking. Seven links succeeded before the stale link
  reduced coverage below the configured threshold.
- 2026-08-20: a deterministic minimal one-episode component test reproduces
  status 440 after moving the source between `/api/pack` and `/api/parse`. The
  final regression uses two episodes to also verify idempotent retry after one
  link already succeeded.
- 2026-08-20: invalidating only the accepted plan retains the stale 30-second
  inventory. Recovery must invalidate both caches.
- 2026-08-20: retry all refreshed links once. `CreateHardlink` accepts an
  existing link to the same file, so links from the first attempt are safe and
  count toward achieved coverage.
- 2026-08-20: check refreshed planned coverage before retry and achieved
  coverage after retry. Do not import a refreshed plan below the configured
  threshold.
- 2026-08-20: independent review found no production defect or hard standards
  violation. Follow-up work centralizes coupled cache invalidation and planned
  coverage checks, makes the missing-source log structured, and locks down the
  one-refresh limit when a source moves again.

## verification notes

- Red command:
  `go test ./internal/http -run 'TestAuthenticatedCandidateLogsEachMatchingClientReleaseAtDebug|TestAuthenticatedParseRefreshesPlanWhenSourceMoves' -count=2`
- Before the fix, both tests fail deterministically.
- The focused candidate, moved-source, and refreshed-threshold tests pass three
  consecutive runs.
- `go test ./internal/http` passes.
- `go test -race ./internal/http` passes.
- `go test -v ./...` passes.
- `go vet ./...` passes.
- `govulncheck ./...` reports no called-code or imported-package
  vulnerabilities. One required-module vulnerability is not called.
- `gofumpt -w .` and `git diff --check` pass.
- Final review found no unbounded retry, threshold bypass, or unrelated file
  modification. The existing untracked release-parser audit was preserved.
- Review follow-up verification: focused authenticated endpoint tests pass
  three consecutive runs, `go test -v ./...`, `go test -race
  ./internal/http`, `go vet ./...`, `govulncheck ./...`, `gofumpt -w .`, and
  `git diff --check` pass.
- The final independent Standards and Spec reviews report no findings.
