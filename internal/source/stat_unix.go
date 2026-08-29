//go:build unix

package source

import (
	"io/fs"
	"syscall"
)

func identity(fi fs.FileInfo) (dev, ino uint64) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0
	}
	return uint64(st.Dev), uint64(st.Ino)
}
