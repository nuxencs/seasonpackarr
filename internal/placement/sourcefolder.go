// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package placement

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/nuxencs/seasonpackarr/internal/errtrace"
)

// ErrSourceOutsideSourceFolder is the cause of a refused hardlink source of a source folder. The path of the source
// does not resolve below the folder with the rule of os.Root, or the link gave another file than the one below the
// folder.
var ErrSourceOutsideSourceFolder = errors.New("the hardlink source is not below its source folder")

// beforeSourceLink runs between the open of a source below its source folder and the link call. It is nil in
// production. A test sets it to change the path of the source after the open.
var beforeSourceLink func()

// StatSourceFolderFile returns the file info of the file at path below folder. It resolves path below folder with the
// rule of os.Root, the rule of Link for a link with a source folder: a relative symlink that stays below folder is
// allowed, each absolute symlink is refused, and folder itself can be a symlink. It does not follow a symlink at path
// itself. A path that leaves folder gives an error that wraps ErrSourceOutsideSourceFolder.
func StatSourceFolderFile(folder, path string) (fs.FileInfo, error) {
	root, rel, err := openSourceFolder(folder, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return lstatBelow(root, rel, path)
}

// openSourceFolder opens folder as a root and returns path relative to it.
func openSourceFolder(folder, path string) (*os.Root, string, error) {
	rel, err := filepath.Rel(folder, path)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, "", errtrace.WithStack(fmt.Errorf("%w: %q is not below %q", ErrSourceOutsideSourceFolder, path, folder))
	}
	root, err := os.OpenRoot(folder)
	if err != nil {
		return nil, "", errtrace.WithStack(err)
	}
	return root, rel, nil
}

// lstatBelow reads rel below root. os.Root has no exported error for a path that leaves the root, so a path that the
// root refuses and the plain call reads leaves the folder or passes an absolute symlink.
func lstatBelow(root *os.Root, rel, path string) (fs.FileInfo, error) {
	info, err := root.Lstat(rel)
	if err == nil {
		return info, nil
	}
	if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fs.ErrPermission) {
		if _, plainErr := os.Lstat(path); plainErr == nil {
			return nil, errtrace.WithStack(fmt.Errorf("%w: %w", ErrSourceOutsideSourceFolder, err))
		}
	}
	return nil, errtrace.WithStack(err)
}

// verifiedHardlink links the source of link to target and proves that the target is the file below the source folder.
// The link call takes the full path, because os.Root can link only when both names are below one root. A folder of
// the path can change between the open and the link, so the call compares the target with the opened file. It removes
// only a link that it made itself. A file that was at the target before is never removed, and when it is the opened
// file, as for a source folder that overlaps the import root, the link counts as linked.
func verifiedHardlink(rootDir *os.Root, link PlannedLink, target hardlinkTarget) error {
	folder, rel, err := openSourceFolder(link.SourceFolder, link.Source)
	if err != nil {
		return err
	}
	defer func() { _ = folder.Close() }()
	if _, err := lstatBelow(folder, rel, link.Source); err != nil {
		return err
	}
	source, err := folder.Open(rel)
	if err != nil {
		return errtrace.WithStack(err)
	}
	defer func() { _ = source.Close() }()
	sourceInfo, err := source.Stat()
	if err != nil {
		return errtrace.WithStack(err)
	}

	if beforeSourceLink != nil {
		beforeSourceLink()
	}
	linkErr := os.Link(link.Source, target.path)
	if linkErr != nil && !errors.Is(linkErr, fs.ErrExist) {
		return errtrace.WithStack(linkError(linkErr))
	}
	targetInfo, err := rootDir.Lstat(target.rel)
	if err != nil {
		return errtrace.WithStack(err)
	}
	if os.SameFile(sourceInfo, targetInfo) {
		return nil
	}
	// Another file at the target stays a conflict.
	if linkErr != nil {
		return errtrace.WithStack(linkErr)
	}
	return errtrace.WithStack(errors.Join(
		fmt.Errorf("%w: the hardlink of %q is not the file below %q", ErrSourceOutsideSourceFolder, link.Source, link.SourceFolder),
		rootDir.Remove(target.rel),
	))
}
