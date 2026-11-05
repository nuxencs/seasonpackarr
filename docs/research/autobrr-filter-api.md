# autobrr filter REST API

This note records the autobrr REST API behavior that `seasonpackarr setup filter` uses to write the filter. ADR-0004 records why the command writes the filter through this API.

## Inspected revision

autobrr [v1.88.0](https://github.com/autobrr/autobrr/releases/tag/v1.88.0), the version that the end-to-end tests pin in `test/harness/compose.autobrr.yaml`. On 2026-11-05, a manual run of `seasonpackarr setup filter` against the v1.88.0 image created the filters, and a read of each filter returned the External entries, their order, and the import action as the command wrote them.

## Authentication

autobrr accepts an API key in the `X-API-Token` header. Source: [`internal/http/middleware.go` line 73](https://github.com/autobrr/autobrr/blob/v1.88.0/internal/http/middleware.go#L73). The user creates the key in autobrr under **Settings**, **API keys**.

## Routes

The filter handler serves `GET /api/filters`, `POST /api/filters`, `GET /api/filters/{id}`, and `PUT /api/filters/{id}`. Source: [`internal/http/filter.go` lines 46-60](https://github.com/autobrr/autobrr/blob/v1.88.0/internal/http/filter.go#L46-L60).

- `POST` answers `201 Created` with the stored filter, which includes its `id`.
- `PUT` answers `200 OK`.

## A create stores only the base settings

`Service.Store` validates the filter and writes the filter row. It does not store External entries or actions. `Service.Update` stores the filter row, the indexers, the External entries, the actions, and the notifications. Source: [`internal/filter/service.go` lines 221-297](https://github.com/autobrr/autobrr/blob/v1.88.0/internal/filter/service.go#L221-L297).

The command therefore creates the base filter with `POST`, reads the stored filter, sets `external` and `actions`, and sends the complete filter back with `PUT`. `PUT` overwrites every field, so all other fields come from the read. ADR-0002 records the same limit for autobrr's filter import.

## Required arrays

The filter table stores `resolutions`, `sources`, `codecs`, and `containers` as `NOT NULL`. Source: [`internal/database/migrations/sqlite/0_base_schema.sql` line 73](https://github.com/autobrr/autobrr/blob/v1.88.0/internal/database/migrations/sqlite/0_base_schema.sql#L73) and the lines after it. A read omits empty arrays, because the fields use `omitempty`, so the command adds them again as empty arrays before the `PUT`.

## External entry order

autobrr reads the External entries ordered by `idx DESC` and runs them in that order, so the entry with the highest `index` runs first. Source: [`internal/database/filter.go` line 846](https://github.com/autobrr/autobrr/blob/v1.88.0/internal/database/filter.go#L846). The candidate check gets index `1` and the match gets index `0`.

## Action headers

The API model of an action has a `webhook_headers` field. Source: [`internal/domain/action.go` line 43](https://github.com/autobrr/autobrr/blob/v1.88.0/internal/domain/action.go#L43). The autobrr Webhook action form has no header field, so the manual path in `docs/AUTOBRR.md` cannot set it. The command sends the token in the `apikey` query parameter, so that both paths give the same filter.

## External entry headers

autobrr splits `webhook_headers` at `;` into headers and each header at `=` into a name and a value. It skips a header that does not split into exactly two parts and gives no error. Source: [`internal/filter/service.go` lines 1193-1204](https://github.com/autobrr/autobrr/blob/v1.88.0/internal/filter/service.go#L1193-L1204). A token that contains `;` or `=` would therefore give a filter that sends no token, so the command rejects such a token.
