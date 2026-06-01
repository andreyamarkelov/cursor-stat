package store

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// CopySQLiteSnapshot copies base.db and -wal -shm siblings to destDir.
// Returns path to the copied main database file.
func CopySQLiteSnapshot(basePath, destDir string) (string, error) {
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return "", err
	}

	files := []string{basePath, basePath + "-wal", basePath + "-shm"}
	for _, src := range files {
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return "", fmt.Errorf("stat %s: %w", src, err)
		}
		dest := filepath.Join(destDir, filepath.Base(src))
		if err := copyFile(src, dest); err != nil {
			return "", fmt.Errorf("copy %s: %w", src, err)
		}
	}

	destBase := filepath.Join(destDir, filepath.Base(basePath))
	if _, err := os.Stat(destBase); err != nil {
		return "", fmt.Errorf("snapshot missing main db: %w", err)
	}
	return destBase, nil
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// DirSize returns total bytes of all files under dir (non-recursive symlinks skipped).
func DirSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}

// FileInfo holds basic file metadata for storage reporting.
type FileInfo struct {
	Path string `json:"path"`
	Size int64  `json:"size_bytes"`
}

// StatFile returns FileInfo when path exists.
func StatFile(path string) (*FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &FileInfo{Path: path, Size: info.Size()}, nil
}
