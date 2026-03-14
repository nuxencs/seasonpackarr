# PRODUCT_SENSE.md

`seasonpackarr` is valuable when it quietly prevents waste.

## Primary User Job

A user with autobrr, qBittorrent, and TV automation wants season packs to be grabbed without redownloading episodes they already have.

## Product Promise

- reduce duplicate bandwidth and storage churn
- preserve release-quality constraints
- keep setup understandable enough for self-hosters
- fail visibly when assumptions break

## Product Smells

- season packs accepted too broadly
- hardlinks created into the wrong location
- unclear config interactions
- provider disagreement hidden from the operator
- test helpers drifting from real webhook behavior

## Prioritization Heuristic

Prefer work that improves one of:

1. matching correctness
2. filesystem safety
3. config clarity
4. operator observability
5. integration confidence with autobrr/qBittorrent
