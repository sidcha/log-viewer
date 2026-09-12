//go:build linux || aix || solaris || illumos || zos

package termcolor

import "golang.org/x/sys/unix"

const (
	ioctlGetTermios = unix.TCGETS
	ioctlSetTermios = unix.TCSETS
)
