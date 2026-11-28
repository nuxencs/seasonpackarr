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

	"github.com/nuxencs/seasonpackarr/internal/config"
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

// folderScan is the last finished scan of one source folder. Its zero value is a folder that no scan could read yet.
type folderScan struct {
	files []sourceFile
	// rejected holds the video paths whose names the parser rejected, so the next scan does not parse them again.
	rejected map[string]struct{}
}

// scanScope selects the folders of a scan.
type scanScope int

const (
	// newFolders scans only the folders that no scan tried yet.
	newFolders scanScope = iota
	// allFolders scans each folder again.
	allFolders
)

// ScheduleSourceFolderScans scans each source folder of the snapshot of cfg again each sourceFolderScanInterval, until
// ctx ends. The startup read makes the first scan, and each next scan starts one interval after the end of the last
// one. After each clean-up, it applies a changed interval and scans the folders that have no scan yet.
func (s *Store) ScheduleSourceFolderScans(ctx context.Context, cfg config.Provider) {
	interval := scanInterval(cfg.Snapshot())
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.reloaded:
			snapshot := cfg.Snapshot()
			if changed := scanInterval(snapshot); changed != interval {
				interval = changed
				// A due time in the past fires the timer at once, and scanIfDue checks the time again.
				timer.Reset(time.Until(s.lastFullScan().Add(interval)))
			}
			s.scanSourceFolders(ctx, snapshot, newFolders)
		case <-timer.C:
			timer.Reset(time.Until(s.scanIfDue(ctx, cfg, interval)))
		}
	}
}

// scanInterval bounds the interval by the minimum, because a config that skipped the load can hold no valid interval.
func scanInterval(cfg domain.Config) time.Duration {
	interval, _ := time.ParseDuration(cfg.SourceFolderScanInterval)
	return max(interval, domain.MinSourceFolderScanInterval)
}

// scanIfDue scans each source folder when no full scan ended within interval, and returns the due time of the next
// full scan. It checks after the wait for a running scan, so a long startup scan moves the next scan.
func (s *Store) scanIfDue(ctx context.Context, cfg config.Provider, interval time.Duration) time.Time {
	if !s.lockScans(ctx) {
		return time.Now()
	}
	defer s.unlockScans()
	if ended := s.lastFullScan(); !ended.IsZero() && time.Since(ended) < interval {
		return ended.Add(interval)
	}
	s.scanFolders(ctx, cfg.Snapshot(), allFolders)
	return time.Now().Add(interval)
}

// scanSourceFolders scans the source folders of cfg in scope, after the end of a running scan.
func (s *Store) scanSourceFolders(ctx context.Context, cfg domain.Config, scope scanScope) {
	if !s.lockScans(ctx) {
		return
	}
	defer s.unlockScans()
	s.scanFolders(ctx, cfg, scope)
}

func (s *Store) lockScans(ctx context.Context) bool {
	select {
	case s.scanning <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (s *Store) unlockScans() {
	<-s.scanning
}

func (s *Store) lastFullScan() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fullScanEnded
}

// scanFolders scans the folders of cfg in scope one at a time, so the walks do not compete for one disk. Clients with
// the same path share the scan. It needs the scan lock.
func (s *Store) scanFolders(ctx context.Context, cfg domain.Config, scope scanScope) {
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
		if scope == newFolders && s.triedSourceFolder(folder) {
			continue
		}
		slices.Sort(clients[folder])
		s.scanSourceFolder(ctx, folder, clients[folder])
	}
	if scope == allFolders {
		s.mu.Lock()
		s.fullScanEnded = time.Now()
		s.mu.Unlock()
	}
}

func (s *Store) triedSourceFolder(folder string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.folderScans[folder]
	return ok
}

// scanSourceFolder replaces the result of folder in one step. A scan that cannot read the folder itself, or that ctx
// stops, replaces nothing.
func (s *Store) scanSourceFolder(ctx context.Context, folder string, clients []string) {
	started := time.Now()
	s.mu.Lock()
	previous := s.folderScans[folder]
	s.mu.Unlock()

	scan, err := s.walkSourceFolder(ctx, folder, previous)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		for _, name := range clients {
			s.log.Warn().Err(err).Str("clientname", name).Str("folder", folder).Msg("could not read source folder")
		}
		s.markTried(folder)
		return
	}

	if s.storeScan(folder, scan) {
		s.log.Info().
			Str("folder", folder).
			Int("episode_files", len(scan.files)).
			Int64("duration_ms", time.Since(started).Milliseconds()).
			Msg("source folder scanned")
	}
}

// storeScan replaces the result of folder, unless a clean-up dropped the folder during the scan. It reports whether
// it stored the result.
func (s *Store) storeScan(folder string, scan folderScan) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.namedLocked(folder) {
		return false
	}
	s.folderScans[folder] = scan
	return true
}

// markTried keeps the last result of folder. A folder with no result gets an empty one, so a reload does not scan it
// again before the next interval.
func (s *Store) markTried(folder string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, tried := s.folderScans[folder]; !tried && s.namedLocked(folder) {
		s.folderScans[folder] = folderScan{}
	}
}

// namedLocked reports whether the config of the last clean-up names folder. It needs Store.mu.
func (s *Store) namedLocked(folder string) bool {
	if s.namedFolders == nil {
		return true
	}
	_, ok := s.namedFolders[folder]
	return ok
}

// walkSourceFolder returns the scan of the episode files below root. It takes the result of each known path from
// previous, so it parses only new names. Only a failed read of root itself is an error.
func (s *Store) walkSourceFolder(ctx context.Context, root string, previous folderScan) (folderScan, error) {
	known := make(map[string]*rls.Release, len(previous.files)+len(previous.rejected))
	for _, file := range previous.files {
		known[file.path] = file.release
	}
	for path := range previous.rejected {
		known[path] = nil
	}

	scan := folderScan{rejected: make(map[string]struct{})}
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
		case entry.Type().IsRegular() && release.HasEpisodeVideoExt(path):
			parsed, ok := known[path]
			if !ok {
				parsed, _ = s.parseEpisodeName(path)
			}
			if parsed == nil {
				scan.rejected[path] = struct{}{}
			} else {
				scan.files = append(scan.files, sourceFile{path: path, release: parsed})
			}
		}
		return nil
	})
	if err != nil {
		return folderScan{}, err
	}
	return scan, nil
}

// sourceFolderFiles returns the number of distinct episode files in the last finished scan of each of folders.
func (s *Store) sourceFolderFiles(folders []string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	// A folder can be below another folder of the same client, so a file can be in two results.
	paths := make(map[string]struct{})
	for _, folder := range folders {
		for _, file := range s.folderScans[folder].files {
			paths[file.path] = struct{}{}
		}
	}
	return len(paths)
}
