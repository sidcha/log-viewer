package tty

import (
	"os"
	"testing"
)

func TestSizeOfANonTerminal(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "x")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, _, ok := Size(f); ok {
		t.Error("a regular file reported a terminal size")
	}
}
