// Package backup archives a Minecraft instance's whole working directory
// (world saves, server.properties, plugins/mods/configs) to a local
// tar.gz. Callers (main.go's command dispatch) are responsible for making
// sure the instance is stopped before calling Create — archiving a live
// server isn't attempted here: RCON's "save-off"/"save-all" pause-writes
// convention is Java Edition-specific and isn't supported by Bedrock
// Dedicated Server's RCON, so it can't be relied on uniformly across
// editions. Stopping first (reusing mcserver's existing gracefulStop, which
// already works correctly on both editions) is simpler and guarantees a
// fully consistent archive regardless of server software. Kept separate
// from mcserver (process lifecycle) per the one-package-per-concern
// convention — archiving and retention are a distinct concern from
// starting and stopping a process.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/codenexus/axon/pulse/internal/mcserver"
)

// excludeNames are entries never included in a backup archive, matched by
// base name at any depth: operational logs/crash artifacts, not world
// state. session.lock and pulse.log commonly live one level under
// working_dir (inside the world folder, and at working_dir root
// respectively) rather than only at the top, so this matches by base name
// rather than restricting to top-level entries.
var excludeNames = map[string]bool{
	"logs":          true,
	"crash-reports": true,
	"session.lock":  true,
	"pulse.log":     true,
}

// Engine creates, deletes, and locates backup archives for Pulse-managed
// instances.
type Engine struct {
	// Root is the backups root directory; archives live at
	// <Root>/<instanceID>/<backupID>.tar.gz, structurally outside every
	// instance's own working_dir.
	Root string

	mu    sync.Mutex
	locks map[string]*sync.Mutex // per-instance serialization for Create/Delete/Restore
}

func NewEngine(root string) *Engine {
	return &Engine{Root: root, locks: make(map[string]*sync.Mutex)}
}

func (e *Engine) lockFor(instanceID string) *sync.Mutex {
	e.mu.Lock()
	defer e.mu.Unlock()
	l, ok := e.locks[instanceID]
	if !ok {
		l = &sync.Mutex{}
		e.locks[instanceID] = l
	}
	return l
}

// Result describes a successfully created backup archive.
type Result struct {
	BackupID       string
	Path           string
	SizeBytes      int64
	ChecksumSHA256 string
}

// PathFor returns the on-disk path for a given instance/backup id, whether
// or not it currently exists.
func (e *Engine) PathFor(instanceID, backupID string) string {
	return filepath.Join(e.Root, instanceID, backupID+".tar.gz")
}

// Create archives cfg.WorkingDir to PathFor(cfg.ID, backupID). Callers must
// ensure the instance is stopped first (see package doc) — Create itself
// has no process-lifecycle awareness.
func (e *Engine) Create(cfg mcserver.InstanceConfig, backupID string) (Result, error) {
	lock := e.lockFor(cfg.ID)
	lock.Lock()
	defer lock.Unlock()

	dest := e.PathFor(cfg.ID, backupID)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return Result{}, fmt.Errorf("create backups dir: %w", err)
	}

	size, checksum, err := archive(cfg.WorkingDir, dest, e.Root)
	if err != nil {
		os.Remove(dest)
		return Result{}, err
	}

	return Result{
		BackupID:       backupID,
		Path:           dest,
		SizeBytes:      size,
		ChecksumSHA256: checksum,
	}, nil
}

// Delete removes a backup archive. Deleting an already-absent backup is not
// an error (idempotent, matches how a retry after a partial failure should
// behave).
func (e *Engine) Delete(instanceID, backupID string) error {
	lock := e.lockFor(instanceID)
	lock.Lock()
	defer lock.Unlock()

	if err := os.Remove(e.PathFor(instanceID, backupID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete backup: %w", err)
	}
	return nil
}

// Restore wipes cfg.WorkingDir and extracts backupID's archive in place —
// an exact rollback, not a merge with whatever's currently there. Callers
// must ensure the instance is stopped first (see package doc) and are
// responsible for taking their own pre-restore safety backup beforehand if
// wanted — Restore itself has no process-lifecycle or safety-net awareness,
// matching Create/Delete.
func (e *Engine) Restore(cfg mcserver.InstanceConfig, backupID string) error {
	lock := e.lockFor(cfg.ID)
	lock.Lock()
	defer lock.Unlock()

	archivePath := e.PathFor(cfg.ID, backupID)
	if _, err := os.Stat(archivePath); err != nil {
		return fmt.Errorf("backup archive not found: %w", err)
	}

	entries, err := os.ReadDir(cfg.WorkingDir)
	if err != nil {
		return fmt.Errorf("read working dir: %w", err)
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(cfg.WorkingDir, entry.Name())); err != nil {
			return fmt.Errorf("clear working dir: %w", err)
		}
	}

	if err := extract(archivePath, cfg.WorkingDir); err != nil {
		return fmt.Errorf("extract archive: %w", err)
	}
	return nil
}

// archive tars+gzips workingDir to dest, excluding entries matched by
// excludeNames and anything resolving under backupsRoot (defensive, in case
// an admin configures a backups root nested inside a working_dir — the
// default configuration keeps them structurally separate so this rarely
// triggers). Returns the resulting archive's size and sha256 checksum,
// computed in the same write pass via io.MultiWriter rather than a second
// read.
func archive(workingDir, dest, backupsRoot string) (size int64, checksum string, err error) {
	absBackupsRoot, absErr := filepath.Abs(backupsRoot)
	if absErr != nil {
		absBackupsRoot = ""
	}

	f, err := os.Create(dest)
	if err != nil {
		return 0, "", fmt.Errorf("create archive: %w", err)
	}

	hasher := sha256.New()
	gz := gzip.NewWriter(io.MultiWriter(f, hasher))
	tw := tar.NewWriter(gz)

	walkErr := filepath.WalkDir(workingDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(workingDir, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		if excludeNames[d.Name()] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if absBackupsRoot != "" {
			if absPath, absErr := filepath.Abs(path); absErr == nil && withinRoot(absPath, absBackupsRoot) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		hdr, hdrErr := tar.FileInfoHeader(info, "")
		if hdrErr != nil {
			return hdrErr
		}
		hdr.Name = filepath.ToSlash(rel)
		if d.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if d.Type().IsRegular() {
			file, openErr := os.Open(path)
			if openErr != nil {
				return openErr
			}
			defer file.Close()
			if _, copyErr := io.Copy(tw, file); copyErr != nil {
				return copyErr
			}
		}
		return nil
	})
	if walkErr != nil {
		f.Close()
		return 0, "", fmt.Errorf("archive %s: %w", workingDir, walkErr)
	}
	if err := tw.Close(); err != nil {
		f.Close()
		return 0, "", fmt.Errorf("close tar writer: %w", err)
	}
	if err := gz.Close(); err != nil {
		f.Close()
		return 0, "", fmt.Errorf("close gzip writer: %w", err)
	}
	if err := f.Close(); err != nil {
		return 0, "", fmt.Errorf("close archive file: %w", err)
	}

	info, statErr := os.Stat(dest)
	if statErr != nil {
		return 0, "", fmt.Errorf("stat archive: %w", statErr)
	}
	return info.Size(), hex.EncodeToString(hasher.Sum(nil)), nil
}

// extract unpacks a tar.gz created by archive() into destDir. Rejects any
// entry whose name would resolve outside destDir — defensive against a
// corrupted or maliciously crafted archive (path traversal / "zip-slip"),
// even though in practice every archive here was produced by this same
// package.
func extract(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()

	absDestDir, err := filepath.Abs(destDir)
	if err != nil {
		return fmt.Errorf("resolve destination: %w", err)
	}

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		target := filepath.Join(absDestDir, filepath.FromSlash(hdr.Name))
		if target != absDestDir && !withinRoot(target, absDestDir) {
			return fmt.Errorf("archive entry %q escapes destination", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create dir %q: %w", hdr.Name, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create parent dir for %q: %w", hdr.Name, err)
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode&0o777))
			if err != nil {
				return fmt.Errorf("create file %q: %w", hdr.Name, err)
			}
			_, copyErr := io.Copy(out, tr)
			closeErr := out.Close()
			if copyErr != nil {
				return fmt.Errorf("write file %q: %w", hdr.Name, copyErr)
			}
			if closeErr != nil {
				return fmt.Errorf("close file %q: %w", hdr.Name, closeErr)
			}
		}
	}
}

// withinRoot reports whether absPath is root itself or nested under it.
func withinRoot(absPath, root string) bool {
	return absPath == root || strings.HasPrefix(absPath, root+string(filepath.Separator))
}
