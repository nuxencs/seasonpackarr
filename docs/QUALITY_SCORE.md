# QUALITY_SCORE.md

Scored on 1-5. `5` means strong confidence with clear docs, tests, and operational guardrails.

## Product Domains

| Domain | Score | Notes | Main Gaps |
| --- | --- | --- | --- |
| Core season-pack matching | 4 | Focused code and tests exist | More regression cases for renamed/weird releases |
| Torrent parsing flow | 4 | Exact plans are built before side effects and covered through authenticated HTTP component tests | Live autobrr-to-client verification and adversarial path cases |
| Smart mode / torrent coverage | 4 | Uses distinct MKV and MP4 episode targets, excludes extra videos, and has focused cache and threshold tests | Broader multi-episode and container corpus cases |
| Config experience | 2 | Rich defaults and schema surface | Migration notes and doc sync need discipline |
| Notifications / observability | 4 | One outcome kind controls log severity, notification filtering, color, and failure detail | Sharper operator playbooks |
| Packaging / release | 4 | CI, Docker, GoReleaser, systemd docs exist | No explicit docs quality gates yet |

## Architectural Layers

| Layer | Score | Notes | Main Gaps |
| --- | --- | --- | --- |
| CLI / entrypoints | 4 | Small and understandable | More task-oriented smoke docs |
| HTTP server / middleware | 4 | Narrow surface, auth gate, health endpoints, and authenticated failure-contract tests | No external autobrr contract harness |
| Processing orchestration | 4 | Candidate discovery, exact planning, import execution, and adapters share one domain outcome with lifecycle tests | More external contract verification |
| Matching logic | 4 | Isolated package plus tests | Edge-case corpus should grow |
| File operations | 2 | Conflict and retry behavior are tested, but torrent-derived target containment is not enforced | Add contained target resolution plus traversal and symlink tests |
| Documentation system | 2 | New harness-oriented structure added | No CI/doc linter/doc gardening yet |

## Improvement Rule

Any change that lowers confidence in a `3` or below area should include compensating docs, tests, or observability.
