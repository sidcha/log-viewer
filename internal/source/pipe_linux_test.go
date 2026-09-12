package source

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// TestWriterOfFindsTheProducer is the lookup that lets lv end a pipeline it was
// reading from, instead of leaving the writer blocked forever on its own input.
func TestWriterOfFindsTheProducer(t *testing.T) {
	// A child that writes once and then blocks, the way dmesg -w does on a
	// quiet kernel: it will never notice the pipe closing on its own.
	cmd := exec.Command("sh", "-c", "echo hello; sleep 60")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	// Wait for the first write, so the child is certainly running.
	buf := make([]byte, 16)
	if _, err := pipe.Read(buf); err != nil {
		t.Fatal(err)
	}

	f, ok := pipe.(*os.File)
	if !ok {
		t.Skip("stdout pipe is not an *os.File on this platform")
	}

	pid := WriterOf(int(f.Fd()))
	if pid == 0 {
		t.Fatal("the writer was not found")
	}
	if pid != cmd.Process.Pid {
		t.Errorf("found pid %d, want the child %d", pid, cmd.Process.Pid)
	}

	if name := CommandOf(pid); name == "" {
		t.Error("the process could not be named")
	}

	// And signalling it actually ends the pipeline.
	if err := Signal(pid, syscall.SIGTERM); err != nil {
		t.Fatalf("Signal: %v", err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the producer did not exit after SIGTERM")
	}
}

// TestWriterOfIgnoresNonPipes guards the other direction: lv must not go
// hunting for a process to signal when its input is an ordinary file.
func TestWriterOfIgnoresNonPipes(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "plain")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if pid := WriterOf(int(f.Fd())); pid != 0 {
		t.Errorf("WriterOf on a regular file = %d, want 0", pid)
	}
	if pid := WriterOf(-1); pid != 0 {
		t.Errorf("WriterOf on a bad descriptor = %d, want 0", pid)
	}
}

// TestWriterOfSkipsTheReadEnd checks the direction test: both ends of the pipe
// have the same inode, and signalling ourselves would be wrong.
func TestWriterOfSkipsTheReadEnd(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	// Only this process holds either end, so there is no other writer to find.
	if pid := WriterOf(int(r.Fd())); pid != 0 {
		t.Errorf("WriterOf = %d, want 0 when we hold both ends", pid)
	}
}

func TestCommandOfUnknownPid(t *testing.T) {
	if name := CommandOf(-1); name != "" {
		t.Errorf("CommandOf(-1) = %q, want empty", name)
	}
}

func TestWritableFlagParsing(t *testing.T) {
	// Sanity check against this process: fd 0 of the test binary is not a
	// writable pipe, but a pipe we made certainly is.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	dir := "/proc/self"
	if _, err := os.Stat(dir); err != nil {
		t.Skip("no /proc")
	}
	if !writable(dir, strconv.Itoa(int(w.Fd()))) {
		t.Error("the write end was not recognised as writable")
	}
	if writable(dir, strconv.Itoa(int(r.Fd()))) {
		t.Error("the read end was recognised as writable")
	}
}
