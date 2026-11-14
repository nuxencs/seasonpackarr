# Notification destinations other than Discord use Shoutrrr URLs, and the native Discord sender stays

Users ask for notification services other than Discord, such as Gotify, ntfy, and Apprise (#114, #191). seasonpackarr sends to every Shoutrrr URL in `notifications.shoutrrr`, through the `github.com/nicholas-fedor/shoutrrr` library. One dependency supplies many services, and the user runs no extra service. We use the `nicholas-fedor` fork because the original `containrrr/shoutrrr` has had no release since v0.8.0 in 2023, and qui, from the same ecosystem, also uses the fork. The native Discord sender and the `notifications.discord` key stay, because Shoutrrr sends plain text and Discord users keep their embeds and their config. One global `notificationLevel` applies to all notification destinations.

## Considered Options

- **A native sender for each service**, as autobrr does: no new dependency and rich formats, but each new service needs a new sender, its tests, and config keys.
- **Apprise API**: little code in seasonpackarr, but every user must run one more container.
- **Move Discord to Shoutrrr and remove `notifications.discord`**: one code path, but a breaking config change, and Discord users lose the embeds.

## Consequences

- Discord has two paths: the native `discord` key with embeds, and a `discord://` Shoutrrr URL with plain text. A user who sets both gets two notifications.
- Users put Shoutrrr URLs in their configs, so a later change of library must accept the same URL format or migrate configs.
- Shoutrrr error text can hold the URL and its tokens. Logs and errors name a destination by its list position and scheme, and the error text has the secret parts of each configured URL removed before it is logged.
