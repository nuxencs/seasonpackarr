# The end-to-end search test drives a real, pinned Prowlarr with a Generic Torznab indexer

The `e2e-search` entry starts a search with `POST /api/search` against the release image, a real Prowlarr, qBittorrent, and the seeder. autobrr does not start. Before this test, only a hand-written fixture and a source audit covered the Prowlarr contract: the indexer JSON, the Torznab XML, and the proxy download links. `Download` accepts only proxy links whose scheme, host, and path match `prowlarrURL`, so a different link shape from Prowlarr fails every torrent download. A Prowlarr upgrade can cause this with no change in seasonpackarr. As in ADR-0002, only the real program proves the contract. The image is pinned by tag and digest in `test/harness/compose.prowlarr.yaml` and bumped by hand. Prowlarr runs with a URL base, which is the harder case for link checks.

The tracker behind Prowlarr is a Generic Torznab indexer that points at a fake Torznab server in the test container. seasonpackarr reads only what Prowlarr returns, and Prowlarr returns the same output for all indexer types.

The test calls the seasonpackarr API, which ADR-0002 does not do. The seam is still the user's entry point: `seasonpackarr search` from cron only sends this request, and the CLI tests cover that step.

## Considered Options

- **A Prowlarr integration test of `internal/prowlarr` only**: faster and easier to debug, but it needs its own Prowlarr overlay and does not cover the search flow or the image wiring.
- **A custom Cardigann definition**: closer to most user setups, but it tests Prowlarr's scraper, not seasonpackarr.
- **The search scenario in the shared scenario list**: all three clients would run it and start Prowlarr. The 30-second inventory cache already makes the scenario order fragile.
- **RSS monitoring in the test**: `rssInterval` has a 10-minute minimum and no startup poll, so the test would need a test-only setting. RSS uses the same Torznab controller and proxy links as search. Unit tests cover the RSS paging.
