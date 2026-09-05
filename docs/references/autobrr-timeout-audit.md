# Autobrr Webhook Timeout Audit

This note records the autobrr behavior that affects the seasonpackarr
`/api/parse` action.

## Inspected Revisions

- Latest autobrr release on 2026-08-09: [v1.83.0](https://github.com/autobrr/autobrr/releases/tag/v1.83.0), commit [`3dd1ac20b39542497fa6f2db0bea7e037edc9dfc`](https://github.com/autobrr/autobrr/tree/3dd1ac20b39542497fa6f2db0bea7e037edc9dfc).
- Autobrr `develop` head on 2026-08-09: commit [`82c8c5096bd432aa85a9cca0b4ea91f7e692511e`](https://github.com/autobrr/autobrr/tree/82c8c5096bd432aa85a9cca0b4ea91f7e692511e).
- Local clone: `/Users/nuxen/dev/oss/autobrr`.

The relevant action timeout, request, and result code is the same in both
revisions.

## Exact Timeout

Autobrr gives Webhook actions a 120-second HTTP client timeout. The Webhook
action uses this client directly and waits for the request to return. The value
is hard-coded. It is not an action setting.

Sources:

- v1.83.0 client configuration: [`internal/action/service.go` lines 49-64](https://github.com/autobrr/autobrr/blob/3dd1ac20b39542497fa6f2db0bea7e037edc9dfc/internal/action/service.go#L49-L64).
- Current client configuration: [`internal/action/service.go` lines 49-64](https://github.com/autobrr/autobrr/blob/82c8c5096bd432aa85a9cca0b4ea91f7e692511e/internal/action/service.go#L49-L64).
- v1.83.0 synchronous request: [`internal/action/run.go` lines 233-258](https://github.com/autobrr/autobrr/blob/3dd1ac20b39542497fa6f2db0bea7e037edc9dfc/internal/action/run.go#L233-L258).

Go defines `http.Client.Timeout` as the complete request time limit. It covers
connection setup, redirects, and response-body reads. The timer continues after
`Do` returns and can interrupt a later body read. Source: [Go `net/http.Client`
documentation](https://pkg.go.dev/net/http#Client).

For the seasonpackarr handler, which does not send the response before import
processing finishes, the practical limit is 120 seconds from the start of the
autobrr HTTP request. An earlier parent-context deadline could reduce this
limit. Autobrr does not configure a longer action-specific grace period.

The complete release-processing task already starts in a goroutine after
autobrr parses the announce. This prevents the IRC announce parser from waiting.
Within that task, actions run in a serial loop and the Webhook action remains
synchronous. Sources: [announce dispatch lines 140-141](https://github.com/autobrr/autobrr/blob/82c8c5096bd432aa85a9cca0b4ea91f7e692511e/internal/announce/announce.go#L140-L141) and [action loop lines 526-560](https://github.com/autobrr/autobrr/blob/82c8c5096bd432aa85a9cca0b4ea91f7e692511e/internal/release/service.go#L526-L560).

External Webhook filters use a separate HTTP client, but it has the same
120-second timeout. Sources: [v1.83.0 `internal/filter/service.go` lines 92-107](https://github.com/autobrr/autobrr/blob/3dd1ac20b39542497fa6f2db0bea7e037edc9dfc/internal/filter/service.go#L92-L107) and [current lines 93-108](https://github.com/autobrr/autobrr/blob/82c8c5096bd432aa85a9cca0b4ea91f7e692511e/internal/filter/service.go#L93-L108).

## Webhook Action Result Semantics

The Webhook action checks only whether the HTTP request returned an error. It
does not inspect `res.StatusCode`. Any HTTP response, including `4xx` or `5xx`,
returns `nil` from the action and is treated as success. A connection error,
request cancellation, or timeout returns an action error.

Sources:

- v1.83.0 Webhook action implementation: [`internal/action/run.go` lines 233-258](https://github.com/autobrr/autobrr/blob/3dd1ac20b39542497fa6f2db0bea7e037edc9dfc/internal/action/run.go#L233-L258).
- Current Webhook action implementation: [`internal/action/run.go` lines 236-261](https://github.com/autobrr/autobrr/blob/82c8c5096bd432aa85a9cca0b4ea91f7e692511e/internal/action/run.go#L236-L261).
- v1.83.0 action status mapping: [`internal/release/service.go` lines 639-666](https://github.com/autobrr/autobrr/blob/3dd1ac20b39542497fa6f2db0bea7e037edc9dfc/internal/release/service.go#L639-L666).

Response-body edge case: autobrr drains the response body before the action
function exits, but it ignores drain errors. If response headers arrive before
the deadline and the timeout occurs only while the body is drained, the action
can still be approved. Source: [`pkg/sharedhttp/http.go` lines 108-113](https://github.com/autobrr/autobrr/blob/82c8c5096bd432aa85a9cca0b4ea91f7e692511e/pkg/sharedhttp/http.go#L108-L113).

This behavior is different from an External Webhook filter. An External
Webhook filter compares the received code with its configured expected status
and rejects the filter on a mismatch. Source: [v1.83.0
`internal/filter/service.go` lines 849-891](https://github.com/autobrr/autobrr/blob/3dd1ac20b39542497fa6f2db0bea7e037edc9dfc/internal/filter/service.go#L849-L891).

## Seasonpackarr Design Consequence

Moving `/api/parse` work to a goroutine and returning early would make the
Webhook action finish quickly, but autobrr would record the action as approved
before the import result exists. A later hardlink or torrent-client failure
could not change that action record. A `202`, `400`, or `500` early response
would all have the same approved result because the action ignores HTTP status
codes.

Keeping `/api/parse` synchronous preserves one useful guarantee: when autobrr
records approval, seasonpackarr has completed the request without a transport
error and before the 120-second limit. It still does not give autobrr a truthful
application-level failure result. If seasonpackarr returns `4xx` or `5xx`
within the timeout, autobrr records approval.

Conclusion: do not move the import to an untracked goroutine only to avoid the
timeout. That changes the meaning from "import request completed" to "import
request was accepted" while autobrr still labels the result as approved. A
truthful asynchronous design needs a durable job and later status check. A
truthful synchronous design needs autobrr Webhook actions to validate a
configured response status, similar to External Webhook filters.
