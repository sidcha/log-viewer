//go:build linux

package source

import (
	"os"

	"golang.org/x/sys/unix"
)

func fadviseDontneed(f *os.File, off, length int64) {
	_ = unix.Fadvise(int(f.Fd()), off, length, unix.FADV_DONTNEED)
}
