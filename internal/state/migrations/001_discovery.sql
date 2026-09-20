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
