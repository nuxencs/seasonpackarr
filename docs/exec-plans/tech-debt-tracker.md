# Tech Debt Tracker

## Active Debt

| Area | Debt | Impact | Suggested Next Step |
| --- | --- | --- | --- |
| Docs automation | No CI checks for doc structure, stale links, or missing index updates | Docs can drift silently | Add a docs validation job and basic link/index checks |
| External E2E verification | The authenticated HTTP and filesystem lifecycle uses a mock client; no external autobrr-to-client harness exists | Upstream workflow or cross-process regressions may escape component tests | Add a containerized external harness after a repeated need or concrete regression |
| Secret logging verification | Auth code avoids echoing submitted tokens, but no test prevents API tokens or client credentials from entering logs | A logging regression could expose operational secrets | Add captured-log contract tests for auth, config, and client failures |
| Filesystem path containment | Torrent-controlled pack and file paths are joined to the import root without containment or symlink-escape validation | A crafted torrent path could create hardlinks outside the configured import root | Add a contained target-path resolver and adversarial traversal, absolute-path, and symlink tests |
| Outcome classification | Expected filter rejections are represented as errors. Candidate and match handlers classify these for informational logs, but import can still log a matching rejection at error level ([issue #194](https://github.com/nuxencs/seasonpackarr/issues/194)) | Normal operation creates false error noise and obscures real failures | Separate outcome class and log severity from the HTTP status contract |
| Asynchronous import jobs | `/api/import` waits for torrent-client checks that can exceed autobrr's hard-coded 120-second Webhook action timeout ([audit](../references/autobrr-timeout-audit.md)) | Autobrr can record a timeout while the import continues, and a bare goroutine would lose work on restart | Persist validated jobs under the existing application data directory, deduplicate by client and info hash, run them through a bounded worker, recover unfinished jobs on startup, return `202` only after durable intake, and preserve the current endpoint, payload, and notification setup so users do not need to reconfigure |
| Partial hardlink cleanup | Successful hardlinks and created directories remain when achieved smart-mode coverage falls below the threshold | Harmless directory entries can remain, but automatic cleanup without proven ownership and containment could delete user-managed paths | Keep cleanup disabled until a design can track file and directory ownership, enforce an import-root boundary, and remove only verified empty directories and request-owned links |

## Rule

Persistent debt belongs here, not buried in chat history.
