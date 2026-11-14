# Shoutrrr notification library

Research date: 2026-11-14

## Decision summary

`notifications.shoutrrr` sends through `github.com/nicholas-fedor/shoutrrr` v0.24.0. ADR-0005 records why seasonpackarr uses this fork. This note records the library contracts that the sender depends on. Each link points to the v0.24.0 tag (`e4692b9`).

## Contracts that seasonpackarr uses

- **One router for each URL.** `router.NewWithOptions` initializes every given URL and fails as a whole when one fails ([router.go](https://github.com/nicholas-fedor/shoutrrr/blob/e4692b904c6942a4aabf027c38c0635a968bc0de/pkg/router/router.go#L87-L106)). seasonpackarr creates one router for each configured URL, so one URL cannot stop the others.
- **Timeout.** A positive `SenderOptions.Timeout` is the exact timeout for each service. Zero uses the budget of the service ([router.go](https://github.com/nicholas-fedor/shoutrrr/blob/e4692b904c6942a4aabf027c38c0635a968bc0de/pkg/router/router.go#L543-L567)). seasonpackarr sets 30 seconds, the same as the Discord sender.
- **Context.** `SendContext` sends to each service with a context from the caller and returns one error for each service, in URL order ([router.go](https://github.com/nicholas-fedor/shoutrrr/blob/e4692b904c6942a4aabf027c38c0635a968bc0de/pkg/router/router.go#L334-L352)).
- **Title.** The `title` send param (`types.TitleKey`) sets the title for the services that have one.
- **Validation.** `Locate` parses the URL and initializes the service without a send ([router.go](https://github.com/nicholas-fedor/shoutrrr/blob/e4692b904c6942a4aabf027c38c0635a968bc0de/pkg/router/router.go#L240-L244)). A URL that does not parse gives `ErrParseURLFailed` without the cause, because the cause can quote the URL ([router.go](https://github.com/nicholas-fedor/shoutrrr/blob/e4692b904c6942a4aabf027c38c0635a968bc0de/pkg/router/router.go#L177-L200)).
- **Logger.** A nil logger discards the output of the services ([router.go](https://github.com/nicholas-fedor/shoutrrr/blob/e4692b904c6942a4aabf027c38c0635a968bc0de/pkg/router/router.go#L537-L540)). seasonpackarr passes nil, because service output can hold URL parts.
- **HTTP client.** When `SenderOptions.HTTPClient` is nil, each service uses its own client, which applies URL settings such as `skiptlsverify`. seasonpackarr sets a client only in tests.

## Secrets in errors

`types.TargetError` names the service by its ID, not by the URL ([target_error.go](https://github.com/nicholas-fedor/shoutrrr/blob/e4692b904c6942a4aabf027c38c0635a968bc0de/pkg/types/target_error.go#L1-L35)). The wrapped service error can still quote URL parts. For example, the Telegram service quotes each chat ID from the query. `internal/shoutrrrurl.Redact` removes the userinfo, the path, the query, and each of their parts of every configured URL from the error text before it is logged or printed. `TestSender_SendErrorsHoldNoSecret` checks this for Gotify, Telegram, ntfy, `discord://`, and `generic://`.

## Dependencies

The library adds the MQTT (`eclipse/paho.golang`), WebSocket (`gorilla/websocket`), XMPP (`mellium.im/xmpp`), and OAuth 2 (`golang.org/x/oauth2`) modules. The binary includes them, because the router registers every service.
