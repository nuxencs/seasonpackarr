# Matching And Hardlinking

## Matching Rules That Matter

Current comparison logic checks:

- resolution
- source
- release group
- cut
- edition
- repack status unless `skipRepackCompare` is enabled
- HDR, optionally simplified
- streaming service collection
- episode identity

## Hardlinking Rules That Matter

- do not link when the pack file is not a real episode video
- do not link when file sizes disagree for episode-to-pack matching
- path naming must reflect the pack folder users actually need, not just the announce title when parsing is enabled
- if a planned source disappears before linking, refresh the client inventory
  and exact plan once before the import fails
- keep the refresh bounded; repeated source movement must not create an
  unbounded retry loop

## Safety Expectations

- changes to fuzzy matching need regression tests
- changes to target path derivation need docs plus concrete verification notes
- if behavior intentionally broadens acceptance, explain the user value and failure tradeoff

## Planning Performance Expectations

- parse each client and torrent episode filename once per exact plan
- index targets by every safety-sensitive episode compatibility field
- preserve torrent target order and first-source deduplication
- request candidate file details once through the torrent-client interface
- keep client-specific batching and concurrency inside each adapter

## Match Diagnostics

Exact matching returns successful links and one diagnostic for every unmatched
torrent target. The indexed success path remains the source of truth. The
diagnostic pass runs only for unmatched targets and must not change selection.

Diagnostics use these classifications:

- `source_episode_not_found`: no parsed client source has the same season and
  episode identity
- `compatibility_mismatch`: the closest same-episode source differs by size,
  container, resolution, or release group
- `duplicate_torrent_target`: the torrent contains another target with the same
  exact compatibility key and only the first target receives a source

Compatibility diagnostics report each differing field with `want` and `got`
values. `want` is the announced torrent target requirement. `got` is the value
from the closest same-episode client source.

Log one summary per unmatched target. Do not log every failed source-target
comparison. The full comparison matrix produces noisy quadratic output and can
undo the indexed planner's performance benefit.

## Open Questions

- whether additional sample/extension filtering is needed beyond current checks
- whether provider disagreement should influence matching decisions more directly
