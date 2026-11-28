// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package placement puts a season pack on disk. It builds each hardlink target
// below the import root, and no other package joins an import root with a
// pack name or a file path of a torrent.
package placement

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/nuxencs/seasonpackarr/internal/errtrace"
	"github.com/nuxencs/seasonpackarr/internal/torrents"
)

// Destination is the import root of a torrent client plus the layout that the
// client expects below it. Two destinations with the same root and layout are
// equal. The root must be an absolute path. With a relative root, each link
// fails and the pack data is not complete.
type Destination struct {
	root       string
	packFolder bool
}

// PackFolderDestination is a destination whose files go into the pack folder
// below root.
func PackFolderDestination(root string) Destination {
	return Destination{root: root, packFolder: true}
}

// FlatDestination is a destination whose files go directly below root, for a
// client layout without a subfolder.
func FlatDestination(root string) Destination {
	return Destination{root: root}
}

// Root returns the import root. The client import must use it as its save
// path, so that the client reads the files where Link put them.
func (d Destination) Root() string {
	return d.root
}

// PlannedLink is one hardlink that an import plan asks for.
type PlannedLink struct {
	// Source is the path on disk of the reusable episode.
	Source string
	// TorrentPath is the path of the file in the torrent, as torrents.File gives it.
	TorrentPath string
	// SourceFolder is the source folder whose scan listed Source, and is empty for a source of a torrent. Link refuses
	// a source whose path does not resolve below it, with an error that wraps ErrSourceOutsideSourceFolder.
	SourceFolder string
}

// LinkStatus is the result class of one planned hardlink.
type LinkStatus int

const (
	// Failed is the zero value, so an outcome that was never set is no success.
	Failed LinkStatus = iota
	// Linked means that the hardlink target is a hardlink to the source. The
	// link is new or is from an earlier attempt.
	Linked
	// SourceMissing means that the link failed and the source is not on disk.
	// The inventory that gave the source is then out of date.
	SourceMissing
)

// Outcome is the result of one planned hardlink.
type Outcome struct {
	Status LinkStatus
	// Target is the hardlink target. It is set only when Status is Linked.
	Target string
	// Err is set unless Status is Linked.
	Err error
}

// ErrDifferentFilesystems is the cause of a failed link whose source and import root are on different filesystems. A
// hardlink cannot cross a filesystem, so a source folder or a torrent save path must be on the filesystem of the
// import root.
var ErrDifferentFilesystems = errors.New("the hardlink source and the import root are on different filesystems")

// Placement is one torrent at one destination.
type Placement struct {
	info        torrents.Info
	destination Destination
}

// New binds a validated torrent info to a destination. It reads nothing from disk.
func New(info torrents.Info, destination Destination) Placement {
	return Placement{info: info, destination: destination}
}

// Link makes the planned hardlinks and returns one outcome for each link in
// input order. A failure of the import root gives each link the same error.
func (p Placement) Link(links []PlannedLink) []Outcome {
	outcomes := make([]Outcome, len(links))
	failAll := func(err error) []Outcome {
		for index := range outcomes {
			outcomes[index] = Outcome{Err: err}
		}
		return outcomes
	}

	// Without a link there is no reason to make the import root.
	if len(links) == 0 {
		return outcomes
	}
	// A single-file torrent has no pack folder, and the import plan stops before it.
	if !p.info.IsDir() {
		return failAll(errtrace.WithStack(fmt.Errorf("torrent %q is not a directory torrent", p.info.Name())))
	}
	rootDir, err := p.openRoot(true)
	if err != nil {
		return failAll(err)
	}
	defer func() { _ = rootDir.Close() }()

	files := make(map[string]struct{})
	for file := range p.info.Files() {
		files[file.Path] = struct{}{}
	}
	for index, link := range links {
		if _, ok := files[link.TorrentPath]; !ok {
			outcomes[index] = Outcome{Err: errtrace.WithStack(
				fmt.Errorf("%q is not a file of torrent %q", link.TorrentPath, p.info.Name()),
			)}
			continue
		}
		outcomes[index] = p.link(rootDir, link)
	}
	return outcomes
}

// Complete reports whether each file of the torrent that is not a padding file
// is a regular file with the expected size at its hardlink target. A client
// skips its full data check only for complete data, so any doubt gives false.
// It reads through os.Root, as Link does, so a hardlink target behind a symlink
// that Link refuses is not complete. It makes nothing on disk.
func (p Placement) Complete() bool {
	if !p.info.IsDir() {
		return false
	}
	rootDir, err := p.openRoot(false)
	if err != nil {
		return false
	}
	defer func() { _ = rootDir.Close() }()

	for file := range p.info.Files() {
		if file.Padding {
			continue
		}
		stat, err := rootDir.Stat(p.target(file.Path).rel)
		if err != nil || !stat.Mode().IsRegular() || stat.Size() != file.Size {
			return false
		}
	}
	return true
}

// openRoot opens the import root, and makes it first when create is set. The
// operator sets the root, so it can be new and can be a symlink. A relative root
// is refused: it names a folder below the working directory of seasonpackarr,
// and the client reads the same value relative to its own working directory.
func (p Placement) openRoot(create bool) (*os.Root, error) {
	root := p.destination.root
	if !filepath.IsAbs(root) {
		return nil, errtrace.WithStack(fmt.Errorf("import root %q is not an absolute path", root))
	}
	if create {
		if err := os.MkdirAll(root, 0o755); err != nil {
			return nil, errtrace.WithStack(err)
		}
	}
	rootDir, err := os.OpenRoot(root)
	if err != nil {
		return nil, errtrace.WithStack(err)
	}
	return rootDir, nil
}

func (p Placement) link(rootDir *os.Root, link PlannedLink) Outcome {
	target := p.target(link.TorrentPath)
	if err := hardlink(rootDir, link, target); err != nil {
		// A refused source is a plain failed link, also when the path names no file.
		if errors.Is(err, ErrSourceOutsideSourceFolder) {
			return Outcome{Err: err}
		}
		if _, statErr := os.Stat(link.Source); errors.Is(statErr, fs.ErrNotExist) {
			return Outcome{Status: SourceMissing, Err: err}
		}
		return Outcome{Err: err}
	}
	return Outcome{Status: Linked, Target: target.path}
}

// hardlinkTarget is one hardlink target in the two forms that a link needs.
type hardlinkTarget struct {
	// rel is relative to the import root, for the calls through os.Root.
	rel string
	// path is the import root joined with rel.
	path string
}

// target is the one place that joins the import root, the pack folder, and the
// path of a file in the torrent. The name and the path passed the check of the
// parse, so the result stays below the root.
func (p Placement) target(torrentPath string) hardlinkTarget {
	rel := filepath.FromSlash(torrentPath)
	if p.destination.packFolder {
		rel = filepath.Join(p.info.Name(), rel)
	}
	return hardlinkTarget{rel: rel, path: filepath.Join(p.destination.root, rel)}
}

// hardlink links the source of link to target below rootDir. os.Root makes the
// target folders, because it rejects a symlink that leaves the root. The link
// call uses the full path, because os.Root can link only when both names are
// below the root, and the source is not. A source of a source folder gets the
// verified link. A source of a torrent is linked as its path resolves, because
// the client reads and writes the data of the torrent through the same path.
func hardlink(rootDir *os.Root, link PlannedLink, target hardlinkTarget) error {
	if err := rootDir.MkdirAll(filepath.Dir(target.rel), 0o755); err != nil {
		return errtrace.WithStack(err)
	}
	if link.SourceFolder != "" {
		return verifiedHardlink(rootDir, link, target)
	}
	source := link.Source

	// A hardlink from an earlier attempt is also success, but another file at
	// the target stays a conflict.
	if err := os.Link(source, target.path); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return errtrace.WithStack(linkError(err))
		}

		sourceInfo, sourceErr := os.Stat(source)
		if sourceErr != nil {
			return errtrace.WithStack(sourceErr)
		}
		targetInfo, targetErr := os.Stat(target.path)
		if targetErr != nil {
			return errtrace.WithStack(targetErr)
		}
		if !os.SameFile(sourceInfo, targetInfo) {
			return errtrace.WithStack(err)
		}
	}

	return nil
}

// linkError names the cause of a failed link between two filesystems, because the text of the system is not clear
// about it.
func linkError(err error) error {
	if errors.Is(err, errNotSameDevice) {
		return fmt.Errorf("%w: %w", ErrDifferentFilesystems, err)
	}
	return err
}
