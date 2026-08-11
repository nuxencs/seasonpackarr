# Processing outcome model

## goal

Give candidate, pack, and parse processing one authoritative outcome model so
HTTP responses, logs, and notifications report rejections separately from
failures without changing the established legacy webhook reason contract.

## scope

- add a domain processing outcome with explicit success, rejection, and failure
  kinds
- preserve every legacy numeric reason code, canonical reason text, and endpoint
  contract
- make the processing stage choose the outcome kind from operational context
- adapt HTTP responses, logs, and Discord notifications from the same outcome
- remove status-code error construction and the duplicate notification status
  classification table
- keep candidate notification suppression explicit at the HTTP boundary
- add contract tests for outcome invariants, HTTP logging, and notification
  filtering and presentation

## non-goals

- replacing the legacy numeric webhook reason codes or their HTTP mapping
- changing release compatibility or smart-mode matching rules
- changing notification configuration values or webhook payload setup
- adding a general workflow framework or speculative extension interfaces

## risks

- **Wire compatibility.** Existing autobrr filters depend on numeric response
  codes. Tests must prove the legacy reason codes and canonical text remain
  unchanged.
- **Notification compatibility.** Existing `MATCH`, `INFO`, and `ERROR` values
  must still select success, rejection, and failure notifications respectively.
- **Ambiguous reason codes.** One reason can appear with different outcome kinds
  in different contexts. Notifications must use the outcome kind and never
  infer severity from the reason number.
- **Lost failure detail.** Failures must always carry a cause for logs and
  Discord error fields, while HTTP keeps canonical reason text.

## steps

1. Define the processing language, invariants, and compatibility boundary.
   [done]
2. Add the domain outcome model and migrate processor return values. [done]
3. Adapt HTTP response and notification policy from the domain outcome. [done]
4. Remove duplicate reason classification and reason-derived error construction.
   [done]
5. Update architecture, lifecycle, reliability, and contract documentation.
   [done]
6. Run focused tests, formatting, repository checks, and vulnerability checks.
   Move this plan to completed. [done]

## decision log

- 2026-08-10: keep legacy webhook reason codes stable. Processing context owns
  the outcome kind because the same reason can represent a rejection or a
  failure.
- 2026-08-10: map success, rejection, and failure to the existing `MATCH`,
  `INFO`, and `ERROR` notification levels. This preserves configuration while
  removing the parallel status-code table.
- 2026-08-10: keep the outcome as a small domain value and keep HTTP and Discord
  as adapters. Do not add an interface around pure in-process classification.
- 2026-08-10: treat reason `445` as a rejection when inspected files do not
  match, and as a failure when client file details cannot be read. Keep the
  reason code stable and include the client cause only for the failure.
- 2026-08-10: keep HTTP failure bodies stable and safe by returning canonical
  reason text. Preserve operational causes in logs and failure notifications.
- 2026-08-10: validate outcomes at the HTTP and Discord adapters. An invalid
  outcome returns HTTP 500 or a sender error and never reaches a transport with
  HTTP status zero.
- 2026-08-10: classify smart-mode shortfalls caused by hardlink faults as
  failures while preserving the below-threshold legacy reason code. A policy
  rejection must not hide an operational cause.

## verification notes

- `gofumpt -w .`, `git diff --check`, and
  `jq empty schemas/config-schema.json` passed.
- `go test -v ./...` and `go vet ./...` passed.
- `go test -race ./internal/domain ./internal/http ./internal/notification
  ./internal/torrents` passed.
- `govulncheck ./...` found no reachable symbol or imported-package
  vulnerabilities. It reports one advisory in an unimported package of a
  required module.
- Authenticated HTTP coverage verifies the stable success and rejection
  response bodies, rejection info logs, failure error logs, and the contextual
  reason `445` classification.
- Notification coverage verifies that the same reason can select `INFO` as a
  rejection or `ERROR` as a failure, with matching color and cause detail.
- PR #244 remained open at head commit
  `986c7c497c8f139ccbe5f36a5fd1acf9412730ee`; this branch starts at that exact
  commit.
