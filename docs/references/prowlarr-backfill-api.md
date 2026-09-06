# Prowlarr Backfill API Audit

Inspected local clone: `~/dev/oss/Prowlarr`.
Revision: `647f0dcc8d86448409fc06db5fa071d99b7aaba0`.
Audit date: 2026-09-05. This is a pinned source audit, not a live service test.

## Contracts Used

- `GET /api/v1/indexer` exposes enable state, protocol, search/pagination support,
  priority, and query parameters under capabilities.
  Source: [IndexerResource.cs](https://github.com/Prowlarr/Prowlarr/blob/647f0dcc8d86448409fc06db5fa071d99b7aaba0/src/Prowlarr.Api.V1/Indexers/IndexerResource.cs)
  and [IndexerCapabilityResource.cs](https://github.com/Prowlarr/Prowlarr/blob/647f0dcc8d86448409fc06db5fa071d99b7aaba0/src/Prowlarr.Api.V1/Indexers/IndexerCapabilityResource.cs).
- `GET /{id}/api` accepts Torznab `t`, `q`, `season`, `cat`, `limit`, and `offset`.
  The controller checks disabled indexers and query limits. HTTP 429 responses
  can include `Retry-After`. It converts result download links to Prowlarr proxy
  links. Source: [NewznabController.cs](https://github.com/Prowlarr/Prowlarr/blob/647f0dcc8d86448409fc06db5fa071d99b7aaba0/src/Prowlarr.Api.V1/Indexers/NewznabController.cs).
- RSS items include title, GUID, link, and enclosure URL. This Prowlarr revision
  does not emit a total-results count. Pagination therefore stops on a short
  page, repeated results, or the explicit page budget.
  Source: [NewznabResults.cs](https://github.com/Prowlarr/Prowlarr/blob/647f0dcc8d86448409fc06db5fa071d99b7aaba0/src/NzbDrone.Core/IndexerSearch/NewznabResults.cs).
- Proxy URLs use `/{id}/download` with an opaque protected link, file name, and API
  key. seasonpackarr validates the origin and indexer path, removes the query API
  key, and authenticates with `X-Api-Key`. No remote response bodies or URLs appear
  in adapter errors. Source: [DownloadMappingService.cs](https://github.com/Prowlarr/Prowlarr/blob/647f0dcc8d86448409fc06db5fa071d99b7aaba0/src/NzbDrone.Core/Download/DownloadMappingService.cs).

## Decision

Use the indexer discovery API plus per-indexer Torznab search. Do not call the
Prowlarr grab endpoint, which sends releases to Prowlarr's download clients.
seasonpackarr must retain control of hardlinks and client import.

Prowlarr can still convert some underlying tracker failures into empty results.
The adapter can report only the error signals returned over HTTP or Torznab.
Prowlarr's own logs remain the source for hidden tracker errors.
