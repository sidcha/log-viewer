package source

import (
	"errors"
	"syscall"
)

// WriterOf cannot identify a pipe's writer on Windows, so lv leaves the
// producer alone there.
func WriterOf(fd int) int { return 0 }

// CommandOf names a process.
func CommandOf(pid int) string { return "" }

// Signal is not supported: Windows has no signals to deliver to a producer.
func Signal(pid int, sig syscall.Signal) error {
	return errors.New("signals are not supported on windows")
}
