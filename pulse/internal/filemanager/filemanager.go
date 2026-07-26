// Package filemanager provides sandboxed browse/upload/delete access to an
// instance's working_dir, for Panel's file management UI. Kept separate
// from mcserver (process lifecycle) and backup (whole-tree archive/
// restore) per the one-package-per-concern convention — this is arbitrary
// single-file/single-directory access to an admin-navigable subtree, a
// distinct concern from either.
package filemanager

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Entry describes one immediate child of a listed directory.
type Entry struct {
	Name      string `json:"name"`        // base name only (e.g. "MyPlugin.jar")
	Path      string `json:"path"`        // full path from workingDir root, forward-slash separated, no leading slash — Panel round-trips this verbatim as the next list/delete/upload target, never joins path segments itself
	IsDir     bool   `json:"is_dir"`
	SizeBytes int64  `json:"size_bytes"`  // 0 for directories
	ModTimeMs int64  `json:"mod_time_ms"` // epoch ms, matching Panel's timestamp convention throughout db/schema.ts
}

// withinRoot reports whether absPath is root itself or nested under it.
// Duplicated locally per this codebase's established convention (see
// backup/engine.go and provision/provision.go's identical private
// helpers, not shared via a common import) — lexical only, doesn't
// dereference symlinks, same limitation those two already carry.
func withinRoot(absPath, root string) bool {
	return absPath == root || strings.HasPrefix(absPath, root+string(filepath.Separator))
}

// resolve turns a Panel-supplied relPath into an absolute path guaranteed
// to be workingDir itself or nested under it. Every exported function in
// this package calls this first and rejects on error before touching the
// filesystem — relPath is admin-controlled and must never be trusted to
// stay inside workingDir on its own.
func resolve(workingDir, relPath string) (string, error) {
	absRoot, err := filepath.Abs(workingDir)
	if err != nil {
		return "", fmt.Errorf("resolve working dir: %w", err)
	}
	target := filepath.Join(absRoot, filepath.FromSlash(relPath))
	if !withinRoot(target, absRoot) {
		return "", fmt.Errorf("path %q escapes working_dir", relPath)
	}
	return target, nil
}

// List returns the immediate children of workingDir/relPath (non-recursive
// — deeper navigation is a fresh List call per directory, not a full
// recursive tree). relPath="" (or ".") lists workingDir itself. Entries
// are sorted directories-first, then alphabetically case-insensitive
// within each group.
func List(workingDir, relPath string) ([]Entry, error) {
	dir, err := resolve(workingDir, relPath)
	if err != nil {
		return nil, err
	}
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	base := strings.Trim(filepath.ToSlash(relPath), "/")
	entries := make([]Entry, 0, len(dirEntries))
	for _, de := range dirEntries {
		info, err := de.Info()
		if err != nil {
			continue // e.g. a symlink target that vanished mid-scan — skip rather than fail the whole listing
		}
		childPath := de.Name()
		if base != "" && base != "." {
			childPath = base + "/" + de.Name()
		}
		entries = append(entries, Entry{
			Name:      de.Name(),
			Path:      childPath,
			IsDir:     de.IsDir(),
			SizeBytes: info.Size(),
			ModTimeMs: info.ModTime().UnixMilli(),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, nil
}

// Delete removes relPath (file or directory, recursively) from workingDir.
// Rejects relPath resolving to workingDir itself — an empty string, ".",
// or a ".."-chain that cancels back out to the root — so there is no wire
// path to "delete the whole instance" through this command. Idempotent:
// deleting an already-gone path is not an error, matching
// backup.Engine.Delete's idempotency.
func Delete(workingDir, relPath string) error {
	target, err := resolve(workingDir, relPath)
	if err != nil {
		return err
	}
	absRoot, err := filepath.Abs(workingDir)
	if err != nil {
		return fmt.Errorf("resolve working dir: %w", err)
	}
	if target == absRoot {
		return fmt.Errorf("refusing to delete working_dir itself")
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("delete %q: %w", relPath, err)
	}
	return nil
}

// Save streams r into workingDir/relPath, atomically (temp file in the
// same directory as the final target, then os.Rename) so a crash or a
// dropped connection mid-transfer can never leave a partial/corrupt file
// where a running server (or a later List) might see it — same approach
// as WritePropertiesFile and SaveConfig. Creates any missing parent
// directories first.
func Save(workingDir, relPath string, r io.Reader) (sizeBytes int64, err error) {
	target, err := resolve(workingDir, relPath)
	if err != nil {
		return 0, err
	}
	absRoot, err := filepath.Abs(workingDir)
	if err != nil {
		return 0, fmt.Errorf("resolve working dir: %w", err)
	}
	if target == absRoot {
		return 0, fmt.Errorf("refusing to overwrite working_dir itself")
	}

	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("create parent directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".*.tmp")
	if err != nil {
		return 0, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	n, copyErr := io.Copy(tmp, r)
	closeErr := tmp.Close()
	if copyErr != nil {
		os.Remove(tmpPath)
		return 0, fmt.Errorf("write file: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return 0, fmt.Errorf("close temp file: %w", closeErr)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		os.Remove(tmpPath)
		return 0, fmt.Errorf("replace %q: %w", relPath, err)
	}
	return n, nil
}
