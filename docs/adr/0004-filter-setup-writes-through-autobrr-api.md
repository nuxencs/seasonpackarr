# Filter setup writes the filter through the autobrr REST API, and the binary holds the filter definition

Most support requests come from filters set up by hand: a wrong order or expected status, a missing indexer, a second client action, or a v0.16 filter that still calls `/api/pack`. Users also follow docs for a different version than the binary that they run. A CLI command now writes the filter. The filter definition is Go code in the binary, so the filter always matches the binary that serves it. The command creates a filter, or with a filter ID updates one in place. An update replaces only the External entries and the actions and keeps all other settings, so the user's indexers, resolutions, and priority stay. The end-to-end tests create their filters with the same code, so they prove the filter that users get. `test/e2e/testdata/filter.json` is removed. `docs/AUTOBRR.md` stays as the manual path, and a test compares its values with the Go definition.

## Considered Options

- **Print a filter JSON for the autobrr import**: needs no autobrr API key, but `POST /api/filters` stores only the base filter and the export removes actions, so the External entries and the import action are lost. This is the same limit that ADR-0002 records for the tests.
- **Fix the autobrr import upstream**: removes the need for an API key, but users on older autobrr versions keep the problem, and an import cannot update an existing filter.
- **Keep `filter.json` as the source and have the command read it**: one more file to embed, and the hand-written guide still drifts from it without a test.

## Consequences

- The command needs an autobrr URL and API key, and the URL that autobrr uses to reach seasonpackarr. The config cannot supply that URL, because the listener address means nothing to autobrr.
- A new filter is disabled and has no indexers. The user selects the indexers and the priority, then enables it, because only the user knows the trackers and the order of their other filters.
