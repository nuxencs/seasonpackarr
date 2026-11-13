# Product

seasonpackarr is valuable when it quietly prevents waste. This page records what the product promises and how to weigh changes against that promise. Terms follow [GLOSSARY.md](../GLOSSARY.md).

## User job

A user with a supported torrent client and TV automation wants season packs without downloading episodes again that they already have. The season pack comes from an autobrr announce, from Prowlarr discovery, or from both.

## Promise

- Reduce duplicate bandwidth and storage use.
- Keep the release-quality constraints that the user's existing episodes have.
- Keep setup understandable for self-hosters.
- Fail visibly when an assumption breaks.

Reliable does not mean that every season pack is accepted. It means predictable outcomes with noisy release names, torrent-client state, and filesystem state:

1. Never create misleading hardlinks without notice.
2. Authenticate requests, and reject malformed requests cleanly.
3. Reject the pack when the match cannot establish exact torrent coverage.
4. Keep config reload and startup behavior clear in the logs.

## Product smells

- Season packs accepted too broadly.
- Hardlinks created in the wrong location.
- Config settings that interact in unclear ways.
- Disagreement between a provider and seasonpackarr that the operator cannot see.
- Test helpers that drift from real autobrr and torrent-client behavior.

## Priorities

Prefer work that improves one of these, in this order:

1. Matching correctness. A false positive costs more than an extra download, so broader matching needs tests and a reason.
2. Filesystem safety. Hardlinks are cheap to create and expensive to clean up after trust is lost.
3. Config clarity. `config.yaml`, the defaults, the schema, and [CONFIGURATION.md](CONFIGURATION.md) are one contract.
4. Operator observability.
5. Confidence in the integration with autobrr, Prowlarr, and the supported torrent clients (qBittorrent, Transmission, Deluge).
