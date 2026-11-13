# End-to-end tests drive a real, pinned autobrr through its announce-process endpoint

The end-to-end tests send each release into a real autobrr container through `POST /api/irc/network/{id}/channel/{channel}/announce/process`. autobrr then runs the filter, renders the external webhook and action templates, calls `/api/candidate` and `/api/match`, downloads the `.torrent` file, and fires the Webhook action to `/api/import`, as it does for users. Only the real program proves that `{{ .TorrentDataRawBytes | js }}` renders into valid JSON, that the expected status `250` passes, and that the torrent download happens before the match call. An autobrr upgrade can break these without a change in seasonpackarr. The image is pinned by tag and digest in `test/harness/compose.e2e.yaml` and bumped by hand, so an upstream change shows up in the bump pull request and not in an unrelated one.

The announce-process endpoint needs an enabled IRC network, but no IRC server or tracker. The network points at a server that does not exist, and autobrr keeps trying to connect to it.

## Considered Options

- **A script that copies autobrr's requests to seasonpackarr**: faster and without a third-party image, but it proves only what the script author thinks autobrr sends. A change in autobrr's template rendering, webhook order, or torrent download would not fail it.
- **autobrr's filter import with one JSON file**: the import stores only the base filter and drops external webhooks and actions, so the tests create the filter and then update it over the REST API.
