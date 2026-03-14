# QUALITY_SCORE.md

Scored on 1-5. `5` means strong confidence with clear docs, tests, and operational guardrails.

## Product Domains

| Domain | Score | Notes | Main Gaps |
| --- | --- | --- | --- |
| Core season-pack matching | 4 | Focused code and tests exist | More regression cases for renamed/weird releases |
| Torrent parsing flow | 3 | Supported and documented in README | More end-to-end verification and failure-case docs |
| Smart mode / metadata enrichment | 3 | TVDB/TVMaze fallback logic exists | Better disagreement handling docs and tests |
| Config experience | 2 | Rich defaults and schema surface | Migration notes/doc sync need discipline and current `mapstructure` vuln exposure needs remediation |
| Notifications / observability | 3 | Logging and Discord hooks exist | Sharper operator playbooks |
| Packaging / release | 4 | CI, Docker, GoReleaser, systemd docs exist | No explicit docs quality gates yet |

## Architectural Layers

| Layer | Score | Notes | Main Gaps |
| --- | --- | --- | --- |
| CLI / entrypoints | 4 | Small and understandable | More task-oriented smoke docs |
| HTTP server / middleware | 4 | Narrow surface, auth gate, health endpoints | Contract tests would help |
| Processing orchestration | 3 | Centralized flow | High complexity concentration in processor path |
| Matching logic | 4 | Isolated package plus tests | Edge-case corpus should grow |
| File operations | 3 | Narrow concern | Needs explicit safety verification guidance |
| Documentation system | 2 | New harness-oriented structure added | No CI/doc linter/doc gardening yet |

## Improvement Rule

Any change that lowers confidence in a `3` or below area should include compensating docs, tests, or observability.
