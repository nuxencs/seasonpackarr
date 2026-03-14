# Tech Debt Tracker

## Active Debt

| Area | Debt | Impact | Suggested Next Step |
| --- | --- | --- | --- |
| Dependency hygiene | `govulncheck` on 2026-03-14 reports `GO-2025-3900` and `GO-2025-3787` in `github.com/go-viper/mapstructure/v2@v2.2.1` via config loading | Possible sensitive-data leakage paths in malformed config/error scenarios | Upgrade the affected dependency chain and re-run `govulncheck` |
| Docs automation | No CI checks for doc structure, stale links, or missing index updates | Docs can drift silently | Add a docs validation job and basic link/index checks |
| Processor complexity | `internal/http/processor.go` concentrates orchestration and risk | Harder review and regression isolation | Split narrower responsibilities over time |
| E2E verification | No checked-in integration harness for webhook + filesystem flow | Behavior regressions may escape unit tests | Add fixture-driven integration tests |
| Security verification | No secret-redaction or path-safety specific tests | Higher confidence cost for auth/path changes | Add focused tests and review checklist items |

## Rule

Persistent debt belongs here, not buried in chat history.
