//go:build !linux

package source

import "syscall"

// WriterOf cannot identify a pipe's writer without /proc, so lv leaves the
// producer alone on these platforms.
func WriterOf(fd int) int { return 0 }

// CommandOf names a process.
func CommandOf(pid int) string { return "" }

// Signal sends sig to pid.
func Signal(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) }
