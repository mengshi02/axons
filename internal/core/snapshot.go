package core

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// SnapshotService manages SQLite database snapshots.
type SnapshotService struct {
	dbPath      string
	snapshotDir string
}

// NewSnapshotService creates a SnapshotService for the given DB path.
func NewSnapshotService(dbPath string) *SnapshotService {
	snapshotDir := filepath.Join(filepath.Dir(dbPath), "snapshots")
	return &SnapshotService{dbPath: dbPath, snapshotDir: snapshotDir}
}

// SnapshotInfo holds metadata about a snapshot.
type SnapshotInfo struct {
	Name      string
	Path      string
	Size      int64
	CreatedAt string
}

// Save copies the current DB file to a named snapshot.
func (s *SnapshotService) Save(name string, force bool) (*SnapshotInfo, error) {
	if err := os.MkdirAll(s.snapshotDir, 0755); err != nil {
		return nil, fmt.Errorf("create snapshot dir: %w", err)
	}
	dest := filepath.Join(s.snapshotDir, name+".db")
	if !force {
		if _, err := os.Stat(dest); err == nil {
			return nil, fmt.Errorf("snapshot %q already exists (use --force to overwrite)", name)
		}
	}
	if err := copyFile(s.dbPath, dest); err != nil {
		return nil, fmt.Errorf("copy db: %w", err)
	}
	info, _ := os.Stat(dest)
	return &SnapshotInfo{
		Name:      name,
		Path:      dest,
		Size:      info.Size(),
		CreatedAt: info.ModTime().Format(time.RFC3339),
	}, nil
}

// Restore replaces the current DB with the named snapshot.
func (s *SnapshotService) Restore(name string) error {
	src := filepath.Join(s.snapshotDir, name+".db")
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return fmt.Errorf("snapshot %q not found", name)
	}
	return copyFile(src, s.dbPath)
}

// List returns all snapshots.
func (s *SnapshotService) List() ([]SnapshotInfo, error) {
	entries, err := os.ReadDir(s.snapshotDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var infos []SnapshotInfo
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".db" {
			continue
		}
		fi, _ := e.Info()
		infos = append(infos, SnapshotInfo{
			Name:      e.Name()[:len(e.Name())-3],
			Path:      filepath.Join(s.snapshotDir, e.Name()),
			Size:      fi.Size(),
			CreatedAt: fi.ModTime().Format(time.RFC3339),
		})
	}
	return infos, nil
}

// Delete removes the named snapshot.
func (s *SnapshotService) Delete(name string) error {
	path := filepath.Join(s.snapshotDir, name+".db")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("snapshot %q not found", name)
	}
	return os.Remove(path)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}