package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sidcha/log-viewer/internal/config"
)

// capture runs f with stdout redirected and returns what it printed.
func capture(t *testing.T, f func() int) (string, int) {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w

	code := f()

	os.Stdout = old
	w.Close()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	r.Close()
	return sb.String(), code
}

// isolate points the configuration levels at a temporary directory.
func isolate(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	t.Setenv("LV_CONFIG", "")

	wd := filepath.Join(dir, "work")
	if err := os.MkdirAll(wd, 0o755); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(wd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
}

func TestConfigSetThenGet(t *testing.T) {
	isolate(t)

	if code := runConfig([]string{"tab-width", "4"}); code != 0 {
		t.Fatalf("set exited %d", code)
	}
	out, code := capture(t, func() int { return runConfig([]string{"tab-width"}) })
	if code != 0 {
		t.Fatalf("get exited %d", code)
	}
	if strings.TrimSpace(out) != "4" {
		t.Fatalf("get printed %q, want 4", out)
	}
}

func TestConfigWritesToTheChosenLevel(t *testing.T) {
	isolate(t)

	if code := runConfig([]string{"--local", "tab-width", "3"}); code != 0 {
		t.Fatalf("set --local exited %d", code)
	}
	if _, err := os.Stat(config.LocalName); err != nil {
		t.Fatalf("local file was not written: %v", err)
	}
	global, err := config.Path(config.Global)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(global); !os.IsNotExist(err) {
		t.Error("--local should not have touched the per-user file")
	}

	// The narrower level wins when both are set.
	if code := runConfig([]string{"--global", "tab-width", "9"}); code != 0 {
		t.Fatal("set --global failed")
	}
	out, _ := capture(t, func() int { return runConfig([]string{"tab-width"}) })
	if strings.TrimSpace(out) != "3" {
		t.Fatalf("effective tab-width = %q, want the local 3", strings.TrimSpace(out))
	}
}

func TestConfigList(t *testing.T) {
	isolate(t)
	if code := runConfig([]string{"wrap", "on"}); code != 0 {
		t.Fatal("set failed")
	}

	out, code := capture(t, func() int { return runConfig([]string{"--list"}) })
	if code != 0 {
		t.Fatalf("--list exited %d", code)
	}
	if !strings.Contains(out, "wrap") || !strings.Contains(out, "tab-width") {
		t.Fatalf("--list output looks wrong:\n%s", out)
	}
	// Every known setting should be listed, not just the ones in a file.
	for _, s := range config.Settings {
		if !strings.Contains(out, s.Name) {
			t.Errorf("--list omitted %q", s.Name)
		}
	}
}

func TestConfigListShowOrigin(t *testing.T) {
	isolate(t)
	if code := runConfig([]string{"--global", "wrap", "on"}); code != 0 {
		t.Fatal("set failed")
	}

	out, _ := capture(t, func() int { return runConfig([]string{"--list", "--show-origin"}) })
	var wrapLine, scrollLine string
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, "wrap "):
			wrapLine = l
		case strings.HasPrefix(l, "scroll-step "):
			scrollLine = l
		}
	}
	if !strings.Contains(wrapLine, "config") {
		t.Errorf("wrap should be traced to a file, got %q", wrapLine)
	}
	if !strings.Contains(scrollLine, "builtin") {
		t.Errorf("an untouched setting should read as builtin, got %q", scrollLine)
	}
}

func TestConfigUnset(t *testing.T) {
	isolate(t)
	if code := runConfig([]string{"tab-width", "4"}); code != 0 {
		t.Fatal("set failed")
	}
	if code := runConfig([]string{"--unset", "tab-width"}); code != 0 {
		t.Fatal("unset failed")
	}

	out, _ := capture(t, func() int { return runConfig([]string{"tab-width"}) })
	if strings.TrimSpace(out) != "8" {
		t.Fatalf("after unset tab-width = %q, want the default 8", strings.TrimSpace(out))
	}
	if code := runConfig([]string{"--unset", "tab-width"}); code == 0 {
		t.Error("unsetting something that is not set should fail")
	}
}

func TestConfigRejectsBadInput(t *testing.T) {
	isolate(t)

	cases := [][]string{
		{"nosuchthing"},
		{"nosuchthing", "1"},
		{"tab-width", "wide"},
		{"--unset"},
		{"--system", "--global", "tab-width", "2"},
		{"a", "b", "c"},
	}
	for _, args := range cases {
		if code := runConfig(args); code == 0 {
			t.Errorf("lv config %v should have failed", args)
		}
	}
}

func TestConfigEditRejectsABrokenFile(t *testing.T) {
	isolate(t)

	// An "editor" that writes something lv will not accept.
	script := filepath.Join(t.TempDir(), "editor.sh")
	body := "#!/bin/sh\necho 'set nosuchthing on' > \"$1\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", script)
	t.Setenv("VISUAL", "")

	if code := runConfig([]string{"--edit"}); code == 0 {
		t.Error("--edit should report a file that lv cannot load")
	}
}

func TestConfigEditNeedsAnEditor(t *testing.T) {
	isolate(t)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	if code := runConfig([]string{"--edit"}); code == 0 {
		t.Error("--edit without $EDITOR should fail with advice")
	}
}
