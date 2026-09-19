// Package atomicfile replaces a file's contents all at once or not at all.
//
// A write goes to a temporary file beside the target, which is renamed over
// it only once everything is on disk. An interrupted or failed write -- a
// full disk, a cancelled export, a crash -- leaves the old file untouched and
// no half-written one in its place.
package atomicfile

import (
	"io"
	"os"
	"path/filepath"
)

// Write creates or replaces path with what fill writes, creating any missing
// directories. The file ends up with mode perm, whatever the umask would have
// given a temporary file.
func Write(path string, perm os.FileMode, fill func(w io.Writer) error) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		if name != "" {
			tmp.Close()
			os.Remove(name)
		}
	}()

	if err := fill(tmp); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	name = "" // adopted
	return nil
}
