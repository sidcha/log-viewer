package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sidcha/log-viewer/internal/filter"
)

// atLevels points every configuration level at a temporary directory so a test
// can exercise the cascade without touching the real machine.
func atLevels(t *testing.T) (global, local string) {
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

	g, err := Path(Global)
	if err != nil {
		t.Fatal(err)
	}
	l, err := Path(Local)
	if err != nil {
		t.Fatal(err)
	}
	return g, l
}

func writeAt(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestLayersCascade is the git-like property: every layer that exists is read,
// and a narrower one overrides a wider one without restating the rest.
func TestLayersCascade(t *testing.T) {
	global, local := atLevels(t)

	writeAt(t, global, "set tab-width 4\nset wrap on\n")
	writeAt(t, local, "set tab-width 2\n")

	c, err := Load(LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if got := c.Int("tab-width"); got != 2 {
		t.Errorf("tab-width = %d, want the local value 2", got)
	}
	if !c.Bool("wrap") {
		t.Error("wrap = off; the global value should survive a local file that does not mention it")
	}
	if got := c.Int("scroll-step"); got != 1 {
		t.Errorf("scroll-step = %d, want the built-in default 1", got)
	}
}

func TestExplicitPathWinsOverEveryLayer(t *testing.T) {
	global, local := atLevels(t)
	writeAt(t, global, "set tab-width 4\n")
	writeAt(t, local, "set tab-width 2\n")

	explicit := filepath.Join(t.TempDir(), "explicit")
	writeAt(t, explicit, "set tab-width 7\n")

	c, err := Load(LoadOptions{Path: explicit})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Int("tab-width"); got != 7 {
		t.Errorf("tab-width = %d, want 7", got)
	}
}

func TestLVConfigEnvIsRead(t *testing.T) {
	global, _ := atLevels(t)
	writeAt(t, global, "set tab-width 4\n")

	env := filepath.Join(t.TempDir(), "from-env")
	writeAt(t, env, "set tab-width 5\n")
	t.Setenv("LV_CONFIG", env)

	c, err := Load(LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Int("tab-width"); got != 5 {
		t.Errorf("tab-width = %d, want 5 from LV_CONFIG", got)
	}
}

func TestNoUserIgnoresEveryLayer(t *testing.T) {
	global, local := atLevels(t)
	writeAt(t, global, "set tab-width 4\n")
	writeAt(t, local, "set tab-width 2\n")

	c, err := Load(LoadOptions{NoUser: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Int("tab-width"); got != 8 {
		t.Errorf("tab-width = %d, want the built-in default 8", got)
	}
}

func TestMissingLayersAreNotAnError(t *testing.T) {
	atLevels(t)
	if _, err := Load(LoadOptions{}); err != nil {
		t.Fatalf("Load with no files present: %v", err)
	}
}

func TestBadLayerIsReported(t *testing.T) {
	_, local := atLevels(t)
	writeAt(t, local, "set nosuchthing on\n")

	if _, err := Load(LoadOptions{}); err == nil {
		t.Fatal("a broken config file should not be ignored")
	}
}

func TestWriteCreatesAndUpdates(t *testing.T) {
	atLevels(t)

	path, err := Write(Global, "tab-width", "4")
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != "set tab-width 4\n" {
		t.Fatalf("file = %q", got)
	}

	// A second write replaces the entry rather than appending a duplicate.
	if _, err := Write(Global, "tab-width", "6"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != "set tab-width 6\n" {
		t.Fatalf("after update file = %q", got)
	}

	if _, err := Write(Global, "wrap", "on"); err != nil {
		t.Fatal(err)
	}
	c, err := Load(LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Int("tab-width") != 6 || !c.Bool("wrap") {
		t.Errorf("written settings did not load back: tab-width=%d wrap=%v", c.Int("tab-width"), c.Bool("wrap"))
	}
}

// TestWriteKeepsTheRestOfTheFile matters because a config file is hand-edited:
// setting one value must not reformat or drop anything else in it.
func TestWriteKeepsTheRestOfTheFile(t *testing.T) {
	atLevels(t)
	path, _ := Path(Global)
	writeAt(t, path, "# my notes\nbind Q quit\nset tab-width 4\n\n# trailing comment\n")

	if _, err := Write(Global, "tab-width", "9"); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, path)
	for _, want := range []string{"# my notes", "bind Q quit", "set tab-width 9", "# trailing comment"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q after write; file is:\n%s", want, got)
		}
	}
	if strings.Contains(got, "tab-width 4") {
		t.Error("the old value is still present")
	}
}

func TestWriteRejectsBadInput(t *testing.T) {
	atLevels(t)
	if _, err := Write(Global, "nosuchthing", "1"); err == nil {
		t.Error("Write should reject an unknown setting")
	}
	if _, err := Write(Global, "tab-width", "wide"); err == nil {
		t.Error("Write should reject a value the parser would refuse")
	}
}

func TestUnset(t *testing.T) {
	atLevels(t)
	if _, err := Write(Global, "tab-width", "4"); err != nil {
		t.Fatal(err)
	}
	path, err := Unset(Global, "tab-width")
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); strings.Contains(got, "tab-width") {
		t.Errorf("file still holds the setting: %q", got)
	}
	if _, err := Unset(Global, "tab-width"); err == nil {
		t.Error("unsetting something that is not set should say so")
	}
	if _, err := Unset(Global, "nosuchthing"); err == nil {
		t.Error("Unset should reject an unknown setting")
	}
}

func TestParseLevel(t *testing.T) {
	for in, want := range map[string]Level{"system": System, "global": Global, "local": Local} {
		got, err := ParseLevel(in)
		if err != nil || got != want {
			t.Errorf("ParseLevel(%q) = (%v, %v)", in, got, err)
		}
	}
	if _, err := ParseLevel("universal"); err == nil {
		t.Error("ParseLevel should reject an unknown level")
	}
	if _, err := Path(Builtin); err == nil {
		t.Error("the built-in level has no file")
	}
}

func TestRuleDirective(t *testing.T) {
	c := New()
	err := c.Parse("t", `
rule only    'ERROR'
rule never   'healthcheck'
rule highlight 'OOM' color=#ff0000
`)
	if err != nil {
		t.Fatal(err)
	}

	presets := c.Presets()
	if len(presets) != 3 {
		t.Fatalf("got %d presets, want 3", len(presets))
	}
	if presets[0].Action != filter.Only || presets[0].Pattern != "ERROR" {
		t.Errorf("preset 0 = %+v", presets[0])
	}
	if presets[1].Action != filter.Never {
		t.Errorf("preset 1 action = %v", presets[1].Action)
	}
	if presets[2].Action != filter.Highlight {
		t.Errorf("preset 2 action = %v", presets[2].Action)
	}
	if got := presets[2].Color; got != (filter.Color{R: 0xff}) {
		t.Errorf("preset 2 colour = %+v, want pure red", got)
	}
	// Presets without a colour are left blank so the viewer assigns one.
	if presets[0].Color != (filter.Color{}) {
		t.Errorf("preset 0 should have no pinned colour, got %+v", presets[0].Color)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
