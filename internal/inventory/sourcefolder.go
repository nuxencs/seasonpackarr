// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package inventory

import (
	"context"
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

// walkSourceFolder returns the episode files below root. It skips each name that starts with a dot and follows no
// symlink below root, so a trash folder, a link loop, or a link out of the tree adds no file. Only a failed read of
// root itself is an error: a subfolder that it cannot read is logged and skipped. It reads names only, no file size
// and no file content.
func (s *Store) walkSourceFolder(ctx context.Context, root string) ([]sourceFile, error) {
	// os.ReadDir follows a symlink at root, so the configured folder itself can be a symlink.
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	var files []sourceFile
	pending := []string{root}
	for len(pending) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		dir := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if dir != root {
			if entries, err = os.ReadDir(dir); err != nil {
				s.log.Warn().Err(err).Str("folder", root).Str("path", dir).Msg("could not read a folder in a source folder; skipped it")
				continue
			}
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			// Type has the symlink bit and no other type bit for a symlink, so a symlink is neither case.
			switch {
			case entry.Type().IsDir():
				pending = append(pending, path)
			case entry.Type().IsRegular():
				if parsed, ok := release.ParseEpisodeName(path); ok {
					files = append(files, sourceFile{path: path, release: parsed})
				}
			}
		}
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
