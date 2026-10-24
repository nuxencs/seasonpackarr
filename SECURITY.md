# Security policy

## Supported versions

Security fixes go into the latest release. Upgrade to the latest release before you report a problem.

## Report a vulnerability

Do not open a public issue for a security problem. Report it privately through [GitHub private vulnerability reporting](https://github.com/nuxencs/seasonpackarr/security/advisories/new).

Include:

- the seasonpackarr version and how you run it (binary, Docker, or systemd)
- the steps to reproduce the problem
- the effect, for example unauthenticated API access or a hardlink outside the import folder

You get an answer in the advisory. After a fix is released, the advisory becomes public.

## Scope

seasonpackarr is a self-hosted service. It has authenticated HTTP endpoints and it changes local files. These areas are in scope:

- access to `/api/candidate`, `/api/match`, `/api/import`, or `/api/search` without a valid API token
- hardlinks or files created outside the configured import folder
- API tokens, API keys, or passwords in logs, notifications, or HTTP responses
- tracker credentials in the database or in torrent download links that other local users can read

Problems in autobrr, Prowlarr, or a torrent client belong to those projects.
