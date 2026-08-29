//go:build !unix

package source

import "io/fs"

// identity cannot distinguish a rotated file from the original on platforms
// without stable inode numbers, so rotation is detected by size alone.
func identity(fi fs.FileInfo) (dev, ino uint64) { return 0, 0 }
