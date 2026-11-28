# Matching And Hardlinking

## Matching Rules That Matter

Current comparison logic checks:

- resolution
- source
- release group
- cut
- edition
- repack status unless `skipRepackCompare` is enabled
- HDR, optionally simplified
- streaming service collection
- episode identity
- "same pack" (status `210` in the candidate check) needs another season pack:
  the series type and the episode number of the pack. An `S01E00` episode has
  the episode number 0 of a season pack, so the type tells the two apart, and
  the episode is compatible

## Hardlinking Rules That Matter

- do not link when the pack file is not a real episode video
- do not link when file sizes disagree for episode-to-pack matching
- count only a client episode file that is on disk: a regular file with the
  size that the client declares. The inventory has no download state, so the
  client also lists an episode that it still downloads
- apply the on-disk rule in each plan build: the match, the plan build of the
  import after a cache miss, the refresh after a missing hardlink source, and a
  search run
- a file of a source folder has its own on-disk rule. The scan reads names
  only, so the plan build reads each candidate file with one stat call
  (`placement.StatSourceFolderFile`). The file is a source only when it is a
  regular file, and its size is the size on disk. The call does not follow a
  symlink at the file itself: the scan follows none below a source folder,
  and on Linux a hardlink of a symlink links the symlink itself. A file that
  is missing or not regular is dropped with the log line
  `client episode file is not reusable` and `source_kind` set to
  `source_folder`. That is not a failed file-detail read, so a discovery
  match does not give status `468` for it
- keep each hardlink source of a source folder below that folder (issue
  #426). The scan follows no symlink below a source folder, but a tool can
  replace a folder of a listed path with a symlink after the scan. Then a
  call with the full path reads a file of the same name and size at another
  place, and the client writes into that file when it repairs a piece of the
  pack. Thus the path of a file must resolve below its source folder with the
  rule of `os.Root`, the same rule as for the hardlink targets: a relative
  symlink that stays below the folder is allowed, each absolute symlink is
  refused, and the source folder itself can be a symlink. The file is checked
  against the source folder whose scan listed it. With nested source folders
  of one client, that is the first folder in the config, and no second folder
  is tried. Each plan build drops a file that fails the rule with the reason
  `outside_source_folder`. `os.Root` has no exported error for a path that
  leaves the root, so a path that the root refuses and that a plain stat call
  reads gets that reason
- the placement module checks a source of a source folder again at link time,
  because a cached import plan or a change after the plan build can give a
  refused path. Each `placement.PlannedLink` of a file carries its source
  folder. A torrent source at the path of a file of a source folder wins, so
  its link carries no source folder. The link is verified: it opens the source below its source folder
  through `os.Root`, makes the link with the full path, and compares the
  hardlink target with the opened file. When they are not the same file, it
  removes the link that it made and fails that link. It removes only a link
  that the same call made, never a file that was at the hardlink target
  before. A refused source fails with an error that wraps
  `placement.ErrSourceOutsideSourceFolder`. It is a plain failed link: it
  starts no plan refresh and adds no status code, and the coverage count and
  the threshold decide the result of the import. One code path serves all
  platforms, with no build tags and no `linkat` call
- a torrent source has no such rule. The save path of a torrent is no
  boundary that the operator declared, and the client reads and writes
  through the same symlink, so the file at that path is the data of the
  torrent. A strict rule would stop the reuse for users whose torrent folder
  is behind a symlink. `TestImport_LinksTorrentSourceBehindSymlinkedFolder`
  pins this behavior
- the file sources go to the episode matcher after the torrent sources, and
  the matcher keeps the first source of a target, so a torrent wins over a
  file for the same episode. The matcher key holds the size, so a file of a
  source folder is linked only when its size equals the size of the file in
  the torrent
- the candidate check reads no filesystem. It uses the file entries of the
  last scan, and a file entry never gives "same pack" (status `210`), because
  only a torrent proves that the client has the pack
- a source folder can overlap the import root, so a file can be at its own
  hardlink target. The link call then fails with "exists", the target is the
  opened source file, and the link counts as linked, as a hardlink from an
  earlier attempt does. The file stays as it is
- a link between two filesystems fails. The placement module wraps the error
  with `placement.ErrDifferentFilesystems`, so the error text says that the
  hardlink source and the import root are on different filesystems
- the on-disk rule is size-only. A file that the client preallocated to its
  full size passes, and the data check of the client during the import finds
  the missing pieces
- build each client episode path from one file-detail read: the save path and
  the file names of a torrent come from the same `GetFiles` call. The inventory
  has no save path, so a client move between the candidate check and the match
  does not give a stale path. A move that is still in progress can drop the
  file, because the file is complete at neither path. The refresh after a
  missing hardlink source covers a move after the match
- path naming must reflect the pack folder users actually need, not just the announce title when parsing is enabled
- if a planned source disappears before linking, refresh the client inventory
  and exact plan once before the import fails. The refresh starts no scan of
  the source folders: the stat call of the plan build drops a file that is gone
- keep the refresh bounded; repeated source movement must not create an
  unbounded retry loop
- keep each hardlink target below the import root. The pack name and the file
  paths come from the torrent, so there are two checks:
  - `torrents.ParseInfo` rejects a torrent when its name is not one folder or
    file name, or when a file path is absolute or has a parent element that
    leaves the pack folder. The match and the import then return status `465`. The
    parse returns the validated type `torrents.Info`. Only the parse can make
    a value of it, so each pack name and file path in an import plan passed
    this check.
  - The placement module (`internal/placement/`) owns the check at link time.
    It is the only code that joins the import root, the pack folder, and a
    file path, and it refuses a link whose path is not a file of the validated
    torrent. `Link` makes the target folders through `os.Root`, which also
    rejects a symlink below the import root that points outside of it. A
    relative symlink that stays below the import root is allowed, and the
    import root itself can be a symlink. `os.Root` rejects each symlink with
    an absolute target, also when the target is below the import root. The
    link call itself uses the full path, because `os.Root` can link only when
    both names are below the root, and the source is not. For a source of a
    source folder, the verified link above proves the result.
- the import root must be an absolute path. The placement module refuses a
  relative root: each link fails with an error that names the root, the
  completeness check answers false, and the import returns status `440` with
  no link made. A relative root names a folder below the working directory of
  seasonpackarr, and the client reads the same value relative to its own
  working directory.
- the completeness check (`Complete`) reads each hardlink target through
  `os.Root`, as the links do, and it makes nothing on disk. A hardlink target
  behind a symlink that `os.Root` rejects reads as incomplete, so the client
  does its full data check. The comparison is size-only: each file that is not
  a padding file must be a regular file with the expected size. Any doubt gives
  the answer "not complete".
- the client adapter decides the import root and the layout (with a pack folder
  or flat) and returns both as one `placement.Destination`. The links, the
  completeness check, and the save path of the client import use that one value

## Safety Expectations

- changes to fuzzy matching need regression tests
- changes to target path derivation need docs plus concrete verification notes
- if behavior intentionally broadens acceptance, explain the user value and failure tradeoff

## Planning Performance Expectations

- parse each client and torrent episode filename once per exact plan
- index targets by every safety-sensitive episode compatibility field
- preserve torrent target order and first-source deduplication
- request candidate file details once through the torrent-client interface
- keep client-specific batching and concurrency inside each adapter

## Match Diagnostics

Exact matching returns successful links and one diagnostic for every unmatched
torrent target. The indexed success path remains the source of truth. The
diagnostic pass runs only for unmatched targets and must not change selection.

Diagnostics use these classifications:

- `source_episode_not_found`: no parsed client source on disk has the same
  season and episode identity. A source that the on-disk rule dropped counts as
  not found, and its own log line `client episode file is not reusable` gives
  the reason
- `compatibility_mismatch`: the closest same-episode source differs by size,
  container, resolution, or release group
- `duplicate_torrent_target`: the torrent contains another target with the same
  exact compatibility key and only the first target receives a source

Compatibility diagnostics report each differing field with `want` and `got`
values. `want` is the announced torrent target requirement. `got` is the value
from the closest same-episode client source.

Log one summary per unmatched target. Do not log every failed source-target
comparison. The full comparison matrix produces noisy quadratic output and can
undo the indexed planner's performance benefit.

## Open Questions

- whether additional sample/extension filtering is needed beyond current checks
- whether provider disagreement should influence matching decisions more directly
