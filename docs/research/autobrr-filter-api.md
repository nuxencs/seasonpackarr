# autobrr filter REST API

This note records the autobrr REST API behavior that `seasonpackarr setup filter` uses to write the filter. ADR-0004 records why the command writes the filter through this API.

## Inspected revision

autobrr [v1.88.0](https://github.com/autobrr/autobrr/releases/tag/v1.88.0), the version that the end-to-end tests pin in `test/harness/compose.autobrr.yaml`. On 2026-11-05, a manual run of `seasonpackarr setup filter` against the v1.88.0 image created the filters, and a read of each filter returned the External entries, their order, and the import action as the command wrote them.

## Authentication

autobrr accepts an API key in the `X-API-Token` header. Source: [`internal/http/middleware.go` line 73](https://github.com/autobrr/autobrr/blob/v1.88.0/internal/http/middleware.go#L73). The user creates the key in autobrr under **Settings**, **API keys**.

## Routes

The filter handler serves `GET /api/filters`, `POST /api/filters`, `GET /api/filters/{id}`, and `PUT /api/filters/{id}`. Source: [`internal/http/filter.go` lines 46-60](https://github.com/autobrr/autobrr/blob/v1.88.0/internal/http/filter.go#L46-L60).

- `GET /api/filters` answers with the list of filters, each with its `id` and `name`. The command uses it to find a filter with the name of a new filter. The list has only the base columns and no `external` or `actions`, so the stale filter search reads each filter with `GET /api/filters/{id}`. Source: [`internal/database/filter.go` lines 100-136](https://github.com/autobrr/autobrr/blob/v1.88.0/internal/database/filter.go#L100-L136).
- `POST` answers `201 Created` with the stored filter, which includes its `id`.
- `PUT` answers `200 OK`.

The action handler serves `DELETE /api/actions/{id}`, which answers `204 No Content`. Source: [`internal/http/action.go` lines 41-42](https://github.com/autobrr/autobrr/blob/v1.88.0/internal/http/action.go#L41-L42). The autobrr web UI removes an action of a filter with this route.

## A create stores only the base settings

`Service.Store` validates the filter and writes the filter row. It does not store External entries or actions. `Service.Update` stores the filter row, the indexers, the External entries, the actions, and the notifications. Source: [`internal/filter/service.go` lines 221-297](https://github.com/autobrr/autobrr/blob/v1.88.0/internal/filter/service.go#L221-L297).

The command therefore creates the base filter with `POST`, reads the stored filter, sets `external` and `actions`, and sends the complete filter back with `PUT`. `PUT` overwrites the base fields, the indexers, and the notifications, so these come from the read. ADR-0002 records the same limit for autobrr's filter import.

## An update keeps the actions that it does not send

`PUT` replaces the External entries: `StoreFilterExternal` deletes the stored entries and inserts the sent entries. Source: [`internal/database/filter.go` line 1761](https://github.com/autobrr/autobrr/blob/v1.88.0/internal/database/filter.go#L1761). `PUT` does not replace the actions: `StoreFilterActions` updates each sent action with an `id`, inserts each sent action without an `id`, and deletes no action. Source: [`internal/database/action.go` line 870](https://github.com/autobrr/autobrr/blob/v1.88.0/internal/database/action.go#L870).

An update with `--filter-id` therefore deletes each stored action with `DELETE /api/actions/{id}` before the `PUT`. If the `PUT` fails after the deletes, the filter has no action and downloads nothing. The other order could leave the old torrent client action next to the import action, so autobrr would add the pack twice.

A read of a filter logs a failed action lookup and returns the filter without actions. Source: [`internal/filter/service.go` lines 172-176](https://github.com/autobrr/autobrr/blob/v1.88.0/internal/filter/service.go#L172-L176). The command then deletes no action, so it reads the filter again after the `PUT` and fails when the filter has more actions than the update sent.

## Required arrays

The filter table stores `resolutions`, `sources`, `codecs`, and `containers` as `NOT NULL`. Source: [`internal/database/migrations/sqlite/0_base_schema.sql` line 73](https://github.com/autobrr/autobrr/blob/v1.88.0/internal/database/migrations/sqlite/0_base_schema.sql#L73) and the lines after it. A read omits empty arrays, because the fields use `omitempty`, so the command adds them again as empty arrays before the `PUT`.

## External entry order

autobrr reads the External entries ordered by `idx DESC` and runs them in that order, so the entry with the highest `index` runs first. Source: [`internal/database/filter.go` line 846](https://github.com/autobrr/autobrr/blob/v1.88.0/internal/database/filter.go#L846). The candidate check gets index `1` and the match gets index `0`.

## Action headers

The API model of an action has a `webhook_headers` field. Source: [`internal/domain/action.go` line 43](https://github.com/autobrr/autobrr/blob/v1.88.0/internal/domain/action.go#L43). The autobrr Webhook action form has no header field, so the manual path in `docs/AUTOBRR.md` cannot set it. The command sends the token in the `apikey` query parameter, so that both paths give the same filter.

## External entry headers

autobrr splits `webhook_headers` at `;` into headers and each header at `=` into a name and a value. It skips a header that does not split into exactly two parts and gives no error. Source: [`internal/filter/service.go` lines 1193-1204](https://github.com/autobrr/autobrr/blob/v1.88.0/internal/filter/service.go#L1193-L1204). A token that contains `;` or `=` would therefore give a filter that sends no token, so the command rejects such a token.

## External test

The filter handler serves `POST /api/filters/external/test`. Source: [`internal/http/filter.go` line 51](https://github.com/autobrr/autobrr/blob/v1.88.0/internal/http/filter.go#L51). The body is one External entry with the fields of the `external` array of a filter. The command sends the candidate check entry of the filter that it wrote, so autobrr sends the request from its own network position. v1.88.0 is the first release with this route. An older autobrr has only the `/{filterID}` routes, so the path does not match a route and the router answers `404`. Source: [`internal/http/filter.go` in v1.84.0](https://github.com/autobrr/autobrr/blob/v1.84.0/internal/http/filter.go). The command reads the filter list first, so a `404` from this route means that autobrr is older than v1.88.0 and not that the autobrr URL is wrong.

`TestExternal` runs the entry against a fixed sample release, `Best.Show.Ever.S18E21.1080p.AMZN.WEB-DL.DDP2.0.H.264-GROUP`, a single episode. Source: [`internal/filter/service.go` lines 1025-1085](https://github.com/autobrr/autobrr/blob/v1.88.0/internal/filter/service.go#L1025-L1085). The route answers `200 OK` with `status`, `expect_status`, `output`, `error`, `success`, and `duration_ms`, also when the check fails. Source: [`internal/domain/filter.go` lines 253-264](https://github.com/autobrr/autobrr/blob/v1.88.0/internal/domain/filter.go#L253-L264).

- `status` is the HTTP status of the webhook response, or `0` when the request failed, for example without a connection. `error` then has the request error.
- `output` is the first 4096 bytes of the response body.
- autobrr waits up to 120 seconds for the webhook. Source: [`internal/filter/service.go` line 110](https://github.com/autobrr/autobrr/blob/v1.88.0/internal/filter/service.go#L110). seasonpackarr waits up to 60 seconds for some torrent clients, so the command waits 130 seconds for the test route and 30 seconds for the other routes.
- `success` is `true` only when `status` is `expect_status`, `250`. The single-episode sample never gets `250` from the candidate check, so the command ignores `success` and maps `status` and `error`.
