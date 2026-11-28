// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package inventory

import (
	"context"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/release"

	"github.com/autobrr/rls"
)

// sourceFile is one episode file of a source folder. The scan reads only its name.
type sourceFile struct {
	path string
	// release is shared by the clients of the folder. Callers must not change it.
	release *rls.Release
}

// scanSourceFolders scans each source folder of cfg one time, one folder at a time, so the walks do not compete for
// one disk. Clients with the same path share the scan.
func (s *Store) scanSourceFolders(ctx context.Context, cfg domain.Config) {
	clients := make(map[string][]string)
	for name, client := range cfg.Clients {
		if client == nil {
			continue
		}
		for _, folder := range client.SourceFolders {
			clients[folder] = append(clients[folder], name)
		}
	}
	for _, folder := range slices.Sorted(maps.Keys(clients)) {
		if ctx.Err() != nil {
			return
		}
		slices.Sort(clients[folder])
		s.scanSourceFolder(ctx, folder, clients[folder])
	}
}

// scanSourceFolder replaces the result of folder in one step. A scan that cannot read the folder itself, or that ctx
// stops, replaces nothing.
func (s *Store) scanSourceFolder(ctx context.Context, folder string, clients []string) {
	started := time.Now()
	files, err := s.walkSourceFolder(ctx, folder)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		for _, name := range clients {
			s.log.Warn().Err(err).Str("clientname", name).Str("folder", folder).Msg("could not read source folder")
		}
		return
	}

	s.mu.Lock()
	s.sourceFiles[folder] = files
	s.mu.Unlock()
	s.log.Info().
		Str("folder", folder).
		Int("episode_files", len(files)).
		Int64("duration_ms", time.Since(started).Milliseconds()).
		Msg("source folder scanned")
}

// walkSourceFolder returns the episode files below root. Only a failed read of root itself is an error.
func (s *Store) walkSourceFolder(ctx context.Context, root string) ([]sourceFile, error) {
	var files []sourceFile
	// os.DirFS follows a symlink at root, and fs.WalkDir follows no symlink below it, so a link loop or a link out of
	// the tree adds no file.
	err := fs.WalkDir(os.DirFS(root), ".", func(name string, entry fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if name == "." {
			return err
		}
		path := filepath.Join(root, filepath.FromSlash(name))
		if err != nil {
			s.log.Warn().Err(err).Str("folder", root).Str("path", path).Msg("could not read a folder in a source folder; skipped it")
			return fs.SkipDir
		}
		switch {
		case strings.HasPrefix(entry.Name(), "."):
			if entry.IsDir() {
				return fs.SkipDir
			}
		case entry.Type().IsRegular():
			if parsed, ok := release.ParseEpisodeName(path); ok {
				files = append(files, sourceFile{path: path, release: parsed})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// sourceFolderFiles returns the number of distinct episode files in the last finished scan of each of folders.
func (s *Store) sourceFolderFiles(folders []string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	// A folder can be below another folder of the same client, so a file can be in two results.
	paths := make(map[string]struct{})
	for _, folder := range folders {
		for _, file := range s.sourceFiles[folder] {
			paths[file.path] = struct{}{}
		}
	}
	return len(paths)
}
