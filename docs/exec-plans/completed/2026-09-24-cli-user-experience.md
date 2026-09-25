# CLI User Experience

Status: complete.

## Intent Lock

**Goal:** Make routine CLI tasks easy to find, run, and understand. Users should
know which inputs a check requires and what the check proves. Users should not
need to repeat settings that the local configuration already contains.

**Core decision:** None. Work is anchored on the user goal.

**Success criteria:**

- With a local configuration and one client, users can run `candidate`, `match`,
  and `import` with only the required release name or torrent file.
- All remote commands support the same connection settings and precedence:
  explicit flags, environment settings, local configuration, built-in defaults.
- `--url` supports a remote service, HTTPS, and a reverse-proxy path.
- Help shows required inputs, short examples, and whether an operation imports.
- A documented first-check flow starts the service, checks a release name, then
  checks a real torrent file. Each step states its prerequisites and result.
- Results explain accepted, rejected, and failed operations in plain text.
  `--json` supplies structured output for scripts.
- Invalid arguments, connection errors, authentication errors, and server
  operation failures return a nonzero exit status.
- Existing connection flags remain accepted. V1 removes the entire `test` group.

**Technical givens:** Go and Cobra; existing authenticated HTTP endpoints;
existing YAML configuration and environment conventions; no new dependencies
unless a concrete requirement needs them.

## Scope And Steps

1. Shared connection settings. Read existing configuration without creating a
   file or starting server components. Select the sole configured client for
   single-client operations; require an explicit choice when several exist.
   Keep search's existing all-client selection.
2. Direct task commands. Expose `candidate <release>`, `match <file.torrent>`,
   and `import <file.torrent>`. Remove the `test` command group for v1.
   Match and import require real torrent files; reject release-name inputs with
   an explanation and a candidate-check example. Remove implicit mock torrent
   generation from user commands. Unify connection flags and add concise help
   examples. Keep version display independent of network access.
3. Useful results. Print readable status descriptions and search summaries.
   Add `--json`, useful error messages, and consistent stdout/stderr handling.
   Preserve search's existing import and preview modes, but state their effects
   clearly in help and output. Keep existing search JSON fields in `--json`.
4. Verify and document. Cover settings precedence, client selection, legacy
   flags, URL handling, argument validation, outcomes, and exit status. Update
   user guides and operational references. Inspect rendered terminal output.

Each step must leave the CLI usable. Record completed work and scope drift below.

## Non-Goals

- Full-screen terminal UI or an interactive setup wizard.
- Named remote connection profiles or a second configuration file.
- Changes to matching, discovery, hardlinks, import policy, or HTTP contracts.
- A change to the default search operation or automated approval prompts.
- Deployment or live imports.

## Risks

- Script migration: readable output replaces default JSON for search. Document
  `--json`; keep its search report fields stable.
- Exit status corrections change scripts that relied on false success.
- Config loading must not create files, start watchers, or require server-only
  settings for a remote CLI connection. Missing explicit config is an error;
  missing implicit config must allow flag-only remote use.
- A listener address such as `0.0.0.0` needs a loopback address for local requests.
- Release-name mock torrents are unsuitable for real imports or exact checks.
  Requiring real files changes legacy inputs; document the migration and keep
  mock torrent construction available to automated tests where needed.
- Credentials must not appear in help output, result output, or error messages.

## Decision Log

- User confirmed all three priorities: repeated settings, command discovery,
  and output. Also review required inputs and the meaning of the `test` group.
- Prefer existing configuration over an additional CLI settings file.
- Preserve current API behavior. Replace
  implicit synthetic match/import inputs with actionable validation errors.
- No product code changed before scope review.
- User approved the scope and a separate worktree. Prioritize simplicity,
  consistent structure, maintainability, and correctness.
- After review, the user requested removal of the entire `test` command group.
  V1 is a breaking release; compatibility aliases are no longer required.
- The user also requested removal of tests for the old command group's presence
  or absence. Coverage focuses on supported commands and their behavior.
- The user authorized shipping at the top of the PR stack after a clean second
  adversarial review. Both Standards and Spec reviews found no actionable issues.

## Verification Notes

- Inspected root, match, and search help with `go run .`.
- Reproduced `go run . test match` printing missing-input text with exit status 0.
- Reproduced a candidate request to a refused local port returning exit status 0.
- Source inspection: webhook helpers discard response details; search prints
  JSON only; release-name match/import inputs generate mock torrents.
- Passed: `go test -race -count=1 ./cmd ./internal/config ./internal/payload ./internal/http`.
- Passed: `go vet` for those packages, `go fix -diff` with no changes, formatting,
  `git diff --check`, schema JSON parsing, and local documentation link checks.
- `govulncheck ./cmd ./internal/config ./internal/payload` found no reachable
  vulnerabilities. Four advisories apply to required modules but not imported
  vulnerable packages or called code.
- `deadcode -test ./...` reports only `EpisodeMatcher.Len`. The unchanged base
  reports the same finding; it is outside this CLI change.
- Uncached binary/API tests verify preview and import flows. Inspected terminal
  help and result layout. A separate process smoke test verified service startup,
  readiness, config authentication, missing-client diagnostics, and SIGTERM shutdown.
- Fixed a request-body drain issue in the cancellation test fixture and added
  synchronization for subprocess assertions against the HTTP client fixture.
  The final uncached race run passed all affected packages.
- Real torrent-client and tracker services were not used. Imports ran only with
  controlled client fixtures and temporary filesystem data.

## Build Log

### Adversarial Review Follow-Up

- Goal: correct the four reproduced review findings without changing matching
  checks, torrent bytes, or HTTP contracts.
- Added `--release` for torrents with generic root names. CLI/API regression
  tests verify exact checks, import, and the unchanged destination folder.
- HTTP 200 pack responses require the API error envelope before they count as
  rejections. HTML and malformed responses return exit code 1.
- Port validation runs after overrides. Unused invalid environment ports no
  longer block explicit ports or URLs.
- Search subprocess tests isolate environment and config discovery. The test
  sets conflicting operator settings to exercise this boundary.
- Removed `test` and its aliases, simplified direct command construction, and
  updated migration guidance. Removed tests for obsolete command names.
- Passed: `go test -race -count=1 ./cmd ./internal/config ./internal/http`,
  focused `go vet`, `go fix -diff` with no changes, formatting, and diff checks.
  Rechecked `./cmd` after removing the obsolete-command test.
- Inspected terminal help and updated the CLI guide, v1 upgrade guide, and
  operational references. All four findings are resolved.

### Step 1: Shared connection settings, aligned

- Added read-only connection projection, shared file discovery and environment
  overrides, explicit flag precedence, and deterministic client selection.
- Local config and environment-only cases pass focused tests. Explicit missing
  config stays an error and does not create files.
- Goal check: a single-client local setup no longer repeats connection flags.

### Step 2: Direct commands, aligned

- Added fresh command instances and direct operations. Removed the `test` group
  and implicit mock inputs from the CLI.
- Exact operations default to the embedded torrent name. `--release` supplies
  a full tracker name when the folder name lacks metadata. Renamed files work.
- Version output is local. Required arguments and help describe each operation.
- Goal check: command names and inputs distinguish checks from real imports.

### Step 3: Results and errors, aligned

- Shared transport preserves proxy paths, rejects redirects, bounds responses,
  and passes cancellation through. Known API tokens are redacted from errors.
- Added readable results, search summaries, and optional JSON output.
- Exit codes: 0 accepted/search complete, 1 failure, 2 rejected single pack.
- Goal check: users can distinguish what a check proves and why it failed.

### Step 4: Verification and documentation, aligned

- Added a first-check guide, flag precedence reference, and migration notes.
- Updated schema descriptions, config comments, and operational references.
- Binary-level tests verify local config, real API authentication, candidate
  rejection, exact checks, and hardlink imports with a controlled client.
- Existing search CLI/API tests now request JSON explicitly.
- All affected checks pass. Local config alone completes the demonstrated
  candidate, match, and import flow. The starting worktree remains unchanged.
- User-approved scope change: remove the `test` group in v1. Real torrent
  clients and trackers remain outside this run.
