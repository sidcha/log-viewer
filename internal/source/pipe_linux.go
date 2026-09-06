//go:build linux

package source

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// WriterOf returns the process id holding the write end of the pipe on fd.
//
// A producer blocked on its own input never notices that lv has gone: the pipe
// only breaks on the next write, and `dmesg -w` on a quiet kernel may not write
// for hours. Until then the shell waits for the pipeline and the terminal
// appears hung. Finding the peer lets lv end it deliberately.
//
// Returns 0 when fd is not a pipe, or when the writer cannot be identified --
// which includes the common case of a producer running as another user, whose
// /proc entries are not readable.
func WriterOf(fd int) int {
	ino := pipeInode(fd)
	if ino == 0 {
		return 0 // not a pipe: a redirected file or a terminal
	}
	self := fmt.Sprintf("pipe:[%d]", ino)

	procs, err := filepath.Glob("/proc/[0-9]*")
	if err != nil {
		return 0
	}
	me := os.Getpid()

	for _, dir := range procs {
		pid, err := strconv.Atoi(filepath.Base(dir))
		if err != nil || pid == me {
			continue
		}
		fds, err := os.ReadDir(filepath.Join(dir, "fd"))
		if err != nil {
			continue // another user's process, or it exited: nothing to see
		}
		for _, entry := range fds {
			target, err := os.Readlink(filepath.Join(dir, "fd", entry.Name()))
			if err != nil || target != self {
				continue
			}
			if writable(dir, entry.Name()) {
				return pid
			}
		}
	}
	return 0
}

// pipeInode is the inode of the pipe behind fd, or 0 if it is not a pipe.
func pipeInode(fd int) uint64 {
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		return 0
	}
	if st.Mode&syscall.S_IFMT != syscall.S_IFIFO {
		return 0
	}
	return uint64(st.Ino)
}

// writable reports whether a process holds this descriptor for writing, so the
// reader at the other end of our own pipe is not mistaken for the producer.
func writable(procDir, fd string) bool {
	b, err := os.ReadFile(filepath.Join(procDir, "fdinfo", fd))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		rest, ok := strings.CutPrefix(line, "flags:")
		if !ok {
			continue
		}
		flags, err := strconv.ParseInt(strings.TrimSpace(rest), 8, 64)
		if err != nil {
			return false
		}
		mode := flags & 0x3 // O_RDONLY, O_WRONLY, O_RDWR
		return mode == int64(os.O_WRONLY) || mode == int64(os.O_RDWR)
	}
	return false
}

// CommandOf names a process, for telling the user what is still holding the
// pipeline open.
func CommandOf(pid int) string {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// Signal sends sig to pid.
func Signal(pid int, sig syscall.Signal) error {
	return syscall.Kill(pid, sig)
}
