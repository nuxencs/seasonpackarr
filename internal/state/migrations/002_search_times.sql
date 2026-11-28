-- The search times: the end of the last answered search query of one search
-- group, for one client, on one indexer.
CREATE TABLE search_times (
    title TEXT NOT NULL,
    season INTEGER NOT NULL,
    client TEXT NOT NULL,
    indexer_id INTEGER NOT NULL CHECK (indexer_id > 0),
    searched_ns INTEGER NOT NULL,
    PRIMARY KEY (title, season, client, indexer_id)
) STRICT;
