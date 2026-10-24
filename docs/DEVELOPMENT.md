# Development

This guide is for contributors. It covers the local setup, the checks to run before a pull request, and the rules that keep code and docs in sync. [CONTRIBUTING.md](../CONTRIBUTING.md) covers the pull request process. [ARCHITECTURE.md](../ARCHITECTURE.md) explains the packages, and [TESTING.md](TESTING.md) explains the test layers.

## Requirements

- Go, at the version in `go.mod`
- Docker, for the integration and end-to-end tests
- `govulncheck` and `gofumpt`

## Build and run

```sh
go build -o seasonpackarr .
go run . start --config <dir>
```

The first start writes a default `config.yaml` into `<dir>` and stops, because the generated client settings are incomplete. Edit the config, then start again. See [INSTALL.md](INSTALL.md#first-configuration).

## Checks

Run the checks for the code you changed:

| Change | Check |
| --- | --- |
| Any Go code | `go test ./...` and `gofumpt -w .` |
| Dependencies or security-sensitive code | `govulncheck ./...` |
| Torrent client adapters | `test/harness/run.sh` (add `all` for every client version, or name entries such as `qbit-4.3.9`) |
| Request flow, planning, hardlinks, client adapters, config loading, or the image | `test/harness/run.sh e2e` (or `e2e-qbit` for one client) |
| Prowlarr discovery | `go test ./internal/state ./internal/prowlarr ./internal/http ./internal/config ./cmd` |

`test/harness/run.sh list [all]` and `test/harness/run.sh list e2e` print the entries. The harness needs only Docker. If Docker or an external service is not available, say in the pull request which checks did not run.

CI runs `go test -v ./...`, the release builds, the Docker builds, and CodeQL on each pull request. `.github/workflows/integration.yml` runs the integration and end-to-end entries when the paths that they test change. Those jobs are not required checks. [TESTING.md](TESTING.md#harness) lists the paths.

## Smoke checks with the CLI

When a change touches the request flow, check it against a running service and a real client:

```sh
go run . start --config <dir>
go run . candidate "<release>" --config <dir> --client "<name>"
go run . match "<file.torrent>" --config <dir> --client "<name>"
go run . import "<file.torrent>" --config <dir> --client "<name>"
```

`import` creates hardlinks and adds the torrent to the client. See [CLI.md](CLI.md) for the inputs and exit codes.

## Keep code and docs in sync

Update these together, in the same pull request:

- **Config:** `config.yaml`, `schemas/config-schema.json`, `internal/config/`, and [CONFIGURATION.md](CONFIGURATION.md).
- **API:** the handlers in `internal/http/`, the status codes in `internal/domain/`, the CLI tests in `internal/http/cli_test.go`, and [API.md](API.md).
- **autobrr filter:** [AUTOBRR.md](AUTOBRR.md) and `test/e2e/testdata/filter.json`. The end-to-end tests use the JSON file. AUTOBRR.md is hand-written.
- **Domain terms:** [GLOSSARY.md](../GLOSSARY.md). Use its terms in code, docs, and logs.

Track known gaps and follow-up work as GitHub issues, not in files.

## Security rules

- Never log API tokens, API keys, passwords, or other secrets.
- Treat the derivation of filesystem target paths as security-sensitive. Torrent-controlled names flow into hardlink paths.
- Treat the database and its backups as sensitive. Cached torrent bytes and download links can contain tracker credentials.
- Record each new outbound network dependency in [research/](research/) with its primary sources.
- Explain in the pull request how you verified changes to auth, paths, or outbound requests.

## Change checklist

- Did request validation stay strict? If an endpoint accepts more or less than before, describe the effect for operators in the pull request.
- Did matching become broader or narrower, and why? Can duplicate or unrelated client episodes broaden acceptance?
- Does cancellation stop planning and polling promptly, without interrupting a started client import?
- Does any client import wait for a client data check? It must not, because autobrr stops the import action after 120 seconds.
- Can shutdown finish within its fixed deadline?
- Which log line would an operator use to debug this?
- Can you exercise the behavior with `candidate`, `match`, or `import`?

## Check Prowlarr discovery on a live installation

Automated fixtures cover the matching rules, duplicate selection, API authentication, and failure handling. Tracker responses, client paths, filesystem mounts, and the scheduler need a check on a real installation. Use a test client with a few completed episode torrents and a known compatible season pack on an enabled tracker. Configure the running service with the real Prowlarr URL and key, and keep `search.rssInterval: "0s"` for the first checks.

1. Run `seasonpackarr search --dry-run --client test --api "<token>"`. Confirm the series, season, and release variant. Set `search.indexerIDs` and check that only those trackers receive queries. Confirm that `candidate` outcomes have null coverage, `torrentDownloads` is zero, and Prowlarr has no grab requests. Check `failures` for tracker or credential errors.
2. Add `--verify` and check the reusable episode counts and smart-mode decisions. Run the same preview again: it must use cached metadata for eligible results. Both previews must leave the client torrents and local folders unchanged.
3. Review every `would_import` result. When the selections are correct, run without `--dry-run` and `--verify`. Confirm that the pack uses the expected destination, category, and tags, and that the client recognizes the reused data and starts the torrent. Check that the source episode files and the pack files are hardlinks and that existing data does not download again.
4. Run the preview again. The imported variant must be rejected as already in the client, also for equivalent results from other trackers.
5. To check RSS monitoring, set `rssInterval: "10m"` on a service with only the test client. Confirm that a poll with `rss: true` appears in the logs after the interval, that Prowlarr receives no title or season query for the poll, and that accepted packs use the expected hardlinks and import policy. Set `rssInterval: "0s"` and confirm that no further poll starts.

## Tools

`deadcode` from tagged `golang.org/x/tools` v0.49.0 crashes on boxed receivers with generic methods ([golang/go#80973](https://github.com/golang/go/issues/80973)). Install the fixed revision. This changes only the tool, not the module dependencies:

```sh
go install golang.org/x/tools/cmd/deadcode@v0.49.1-0.20260828025639-2e922938d07f
deadcode -test ./...
```
