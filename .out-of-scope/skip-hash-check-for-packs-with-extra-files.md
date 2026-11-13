# Skip the hash check for season packs with extra files

seasonpackarr does not try to skip or shorten the torrent client's hash check for a season pack that has files other than episodes, such as `.nfo` files, subtitles, or samples.

## Background

seasonpackarr reuses only `.mkv` and `.mp4` episode files. When every file of a pack is already on disk, the client can add the pack without a hash check. A pack with extra files is never complete on disk, so the client checks the present data and downloads the extra files. For a large 4K pack, the check can read the disk for minutes. The import does not wait for this check, so it does not slow down autobrr.

The idea was to let the client skip the check for the reused episode files and download only the extra files.

## Why this is out of scope

The client must hold the complete, verified content of the torrent. Only then can it seed the pack and can other tools cross-seed it. A client that skips the check for some files, or that marks data as complete without reading it, can end up with a pack that does not match the torrent. That pack fails as a cross-seed source and can send bad data to peers.

The hash check is the client's proof that the data on disk matches the torrent. Disk reads are a smaller cost than an incomplete or unverified pack.

## Prior requests

- Tech-debt tracker row "Complete packs with extra files", removed in #289
