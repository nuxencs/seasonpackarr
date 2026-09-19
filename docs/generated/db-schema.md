# SQLite Discovery Schema

Schema version: `1`, tracked by `PRAGMA user_version`.

`internal/state/store.go` embeds and applies the migrations in
`internal/state/migrations/` inside a transaction. Newer schema versions are
rejected, not overwritten. The application uses one connection, WAL journal
mode, SQLite-managed automatic checkpoints, and FULL synchronization. FULL
synchronizes each committed WAL transaction, rather than only checkpoints.
No database server or SQLite CLI is required at runtime.

## Stored State

| Table | Purpose |
| --- | --- |
| `connection` | One fingerprint of the Prowlarr URL and API key. A change atomically clears discovery state. |
| `cooldowns` | Retry deadlines. Indexer `0` denotes the whole connection. |
| `metadata` | Validated torrent bytes, expiry, and last access for bounded LRU eviction. |
| `rss_checkpoints` | At most one page of result identities per eligible indexer. |
| `rss_candidates` | Bounded retained result JSON, expiry, and original insertion order. |

Timestamps use Unix nanoseconds. Result identities hash the GUID (or download
link) and title. Indexer IDs keep tracker results separate. RSS results without
another identity use the title hash. Candidate JSON contains only the fields in
`prowlarr.Result`, not acceptance decisions. Checkpoints and candidates are saved
in one transaction before imports start.

The store does not contain config, client inventory, exact plans, or import jobs.
See [operator guidance](../product-specs/prowlarr-backfill.md#persistent-discovery-state)
for locations, sensitive data, backups, and recovery.

## Generated Schema

Generate this SQL excerpt from the initial migration, from the repository root:

```sh
sqlite3 :memory: '.read internal/state/migrations/001_discovery.sql' '.schema'
```

```sql
CREATE TABLE connection (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    fingerprint BLOB NOT NULL CHECK (length(fingerprint) = 32)
) STRICT;
CREATE TABLE cooldowns (
    indexer_id INTEGER PRIMARY KEY CHECK (indexer_id >= 0),
    until_ns INTEGER NOT NULL
) STRICT;
CREATE TABLE metadata (
    indexer_id INTEGER NOT NULL CHECK (indexer_id > 0),
    identity BLOB NOT NULL CHECK (length(identity) = 32),
    data BLOB NOT NULL,
    expires_ns INTEGER NOT NULL,
    used_ns INTEGER NOT NULL,
    PRIMARY KEY (indexer_id, identity)
) STRICT;
CREATE INDEX metadata_lru ON metadata (used_ns, indexer_id, identity);
CREATE TABLE rss_checkpoints (
    indexer_id INTEGER NOT NULL CHECK (indexer_id > 0),
    identity BLOB NOT NULL CHECK (length(identity) = 32),
    PRIMARY KEY (indexer_id, identity)
) STRICT;
CREATE TABLE rss_candidates (
    indexer_id INTEGER NOT NULL CHECK (indexer_id > 0),
    identity BLOB NOT NULL CHECK (length(identity) = 32),
    result BLOB NOT NULL,
    expires_ns INTEGER NOT NULL,
    sequence INTEGER NOT NULL,
    PRIMARY KEY (indexer_id, identity)
) STRICT;
```
