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

## Safety Expectations

- changes to fuzzy matching need regression tests
- changes to target path derivation need docs plus concrete verification notes
- if behavior intentionally broadens acceptance, explain the user value and failure tradeoff

## Open Questions

- whether additional sample/extension filtering is needed beyond current checks
- whether provider disagreement should influence matching decisions more directly
