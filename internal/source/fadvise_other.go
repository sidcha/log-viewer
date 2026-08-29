//go:build !linux

package source

import "os"

func fadviseDontneed(f *os.File, off, length int64) {}
