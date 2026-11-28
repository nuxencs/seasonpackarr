# Source folders get their release names from file names, and a timer scans them

Users also get episodes from Usenet, and those files never reach the torrent client, so the inventory does not have them. A client can now name source folders, and their episode files join its inventory. seasonpackarr reads the release name of each file from its file name alone, because that needs no second service and works for a folder that any tool filled. A background scan reads the folders at startup, then on an interval, and before each search, because a scan of names is cheap and works on each filesystem. A search waits for its scan. Another request reads the last finished scan and never waits for one.

## Considered Options

- **The Sonarr API for the release name**: Sonarr keeps the original release name of each imported file (`sceneName`), so each naming format would match. It needs a Sonarr URL and API key, a path mapping between Sonarr and seasonpackarr, and a request to Sonarr in the candidate check. It has no name for a manual import, and it ties the feature to one tool. It can be added later as a second way to name the files of a source folder.
- **A file watcher**, as cross-seed uses: a new file counts at once. A watcher sees no change that another host makes on NFS or SMB, needs one watch for each folder on Linux, and needs a full scan as a fallback anyway.
- **A walk in each request**: always fresh, but a walk of a large tree can take longer than autobrr waits for the candidate check.
- **A scan that a Sonarr webhook starts**, as qui offers: fresh without a short interval, but a second setup step in Sonarr. It can be added beside the timer.

## Consequences

- A file name must hold the release variant. A name in the default Sonarr format (`Show - S01E02 - Title WEBDL-1080p.mkv`) has no release group, and an obfuscated name has no episode, so both give no match. This is a safe failure: an extra download, not a wrong hardlink.
- A file name with a year (`Show (2023) - S01E02 ...`) is another season than a pack name with no year, unless `fuzzyMatching.skipYearCompare` is set. That setting is global.
- A new file counts up to one scan interval late. A stale entry does no harm, because the match checks each file on disk.
- A file stays in a source folder after its torrent is gone. Thus each season in a source folder stays a target for discovery, and a pack that the user removed from the client is imported again when an announce, an RSS poll, or a search offers it.
