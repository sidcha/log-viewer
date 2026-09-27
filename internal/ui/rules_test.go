package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/color"
	"github.com/sidcha/log-viewer/internal/filter"
	"github.com/sidcha/log-viewer/internal/ruleset"
)

// quitKey presses q, which is what a user reaches for.
func quitKey(a *App) { keys(a, "q") }

func TestQuitIsSilentWithNothingToLose(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))

	quitKey(a)
	if a.confirm != nil {
		t.Fatal("asked about rules when there are none")
	}
	if !a.quit {
		t.Fatal("did not quit")
	}
}

func TestQuitAsksWhenRulesExist(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))
	a.addRule(filter.Only, "ERROR", color.RGB{})
	waitFiltered(t, a)

	quitKey(a)
	if a.quit {
		t.Fatal("quit without asking, losing the rules")
	}
	if a.confirm == nil {
		t.Fatal("no question was asked")
	}
	if !strings.Contains(a.confirm.text, "Save rules") {
		t.Errorf("question was %q", a.confirm.text)
	}

	// Cancelling leaves everything alone.
	keys(a, "c")
	if a.quit || a.confirm != nil {
		t.Error("cancel did not put things back")
	}
	if a.rules.Len() != 1 {
		t.Error("cancel lost the rules")
	}
}

// TestSearchAloneDoesNotBlockQuit: searching hides nothing and is replaced by
// the next pattern typed, so it is not work to be saved.
func TestSearchAloneDoesNotBlockQuit(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))
	a.doSearch("ERROR", false)

	quitKey(a)
	if a.confirm != nil {
		t.Fatalf("a search raised %q", a.confirm.text)
	}
	if !a.quit {
		t.Fatal("did not quit")
	}
}

// TestSearchDoesNotDirtyASavedSet checks the same thing against a loaded file:
// searching must not make lv think the file needs updating.
func TestSearchDoesNotDirtyASavedSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	if err := os.WriteFile(path, []byte(`{"rules":[{"action":"only","pattern":"ERROR"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	a, _ := newTestApp(t, numbered(60))
	if err := a.LoadRules(path); err != nil {
		t.Fatal(err)
	}
	waitFiltered(t, a)

	a.doSearch("line 01", false)
	if a.rulesDirty() {
		t.Error("a search marked the loaded rules as changed")
	}
	quitKey(a)
	if a.confirm != nil {
		t.Fatalf("a search raised %q", a.confirm.text)
	}
}

func TestEscapeCancelsTheQuestion(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))
	a.addRule(filter.Only, "ERROR", color.RGB{})

	quitKey(a)
	a.key(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if a.quit || a.confirm != nil {
		t.Error("Escape did not cancel cleanly")
	}
}

// TestStrayKeyDoesNotAnswer guards the destructive answer: only the listed
// keys count, so leaning on the keyboard cannot discard the rules.
func TestStrayKeyDoesNotAnswer(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))
	a.addRule(filter.Only, "ERROR", color.RGB{})

	quitKey(a)
	keys(a, "zqj8")
	if a.quit {
		t.Fatal("an unlisted key answered the question")
	}
	if a.confirm == nil {
		t.Fatal("the question went away without being answered")
	}
}

func TestQuitDiscard(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))
	a.addRule(filter.Only, "ERROR", color.RGB{})

	quitKey(a)
	keys(a, "n")
	if !a.quit {
		t.Fatal("discard did not quit")
	}
}

func TestQuitForceNeverAsks(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))
	a.addRule(filter.Only, "ERROR", color.RGB{})

	keys(a, "ZQ")
	if a.confirm != nil {
		t.Fatal("ZQ asked")
	}
	if !a.quit {
		t.Fatal("ZQ did not quit")
	}
}

func TestQuitSaveAsThenQuit(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))
	a.addRule(filter.Only, "ERROR", color.RGB{})
	a.addRule(filter.Highlight, "bad", color.RGB{})

	path := filepath.Join(t.TempDir(), "rules.json")

	quitKey(a)
	keys(a, "y") // save as…
	if a.prompt == nil || a.prompt.kind != pSaveRules {
		t.Fatal("save-as did not open a path prompt")
	}
	typeInto(a, path)
	press(a, tcell.KeyEnter)

	if !a.quit {
		t.Fatal("saving did not then quit")
	}
	f, err := ruleset.Load(path)
	if err != nil {
		t.Fatalf("the file was not written: %v", err)
	}
	if len(f.Rules) != 2 {
		t.Fatalf("wrote %d rules, want 2", len(f.Rules))
	}
}

// TestFailedSaveDoesNotQuit is the case that would otherwise lose everything:
// a bad path must leave the session alive so the user can try again.
func TestFailedSaveDoesNotQuit(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))
	a.addRule(filter.Only, "ERROR", color.RGB{})

	file := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	quitKey(a)
	keys(a, "y")
	typeInto(a, filepath.Join(file, "rules.json")) // a file used as a directory
	press(a, tcell.KeyEnter)

	if a.quit {
		t.Fatal("quit despite the save failing")
	}
	if !strings.Contains(a.msg, "save rules") {
		t.Errorf("no complaint about the failed save: %q", a.msg)
	}
}

func TestLoadedRulesOfferToUpdateInPlace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	if err := os.WriteFile(path, []byte(`{"rules":[{"action":"only","pattern":"ERROR"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	a, _ := newTestApp(t, numbered(60))
	if err := a.LoadRules(path); err != nil {
		t.Fatal(err)
	}
	waitFiltered(t, a)
	if a.rules.Len() != 1 {
		t.Fatalf("loaded %d rules, want 1", a.rules.Len())
	}

	// Unchanged, quitting should not ask at all.
	quitKey(a)
	if a.confirm != nil {
		t.Fatal("asked about an untouched file")
	}
	if !a.quit {
		t.Fatal("did not quit")
	}

	// Change something and it offers to update that same file.
	a.quit = false
	a.addRule(filter.Never, "INFO", color.RGB{})
	waitFiltered(t, a)

	quitKey(a)
	if a.confirm == nil {
		t.Fatal("a change did not raise the question")
	}
	if !strings.Contains(a.confirm.text, "rules.json") {
		t.Errorf("question does not name the file: %q", a.confirm.text)
	}

	keys(a, "y") // update
	if !a.quit {
		t.Fatal("update did not quit")
	}
	f, err := ruleset.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Rules) != 2 {
		t.Fatalf("file has %d rules, want the updated 2", len(f.Rules))
	}
}

func TestLoadedRulesSaveAsElsewhere(t *testing.T) {
	dir := t.TempDir()
	orig := filepath.Join(dir, "orig.json")
	if err := os.WriteFile(orig, []byte(`{"rules":[{"action":"only","pattern":"ERROR"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	a, _ := newTestApp(t, numbered(60))
	if err := a.LoadRules(orig); err != nil {
		t.Fatal(err)
	}
	a.addRule(filter.Never, "INFO", color.RGB{})
	waitFiltered(t, a)

	other := filepath.Join(dir, "other.json")
	quitKey(a)
	keys(a, "a") // save as…
	if a.prompt == nil || a.prompt.kind != pSaveRules {
		t.Fatal("save-as did not open a prompt")
	}
	// The prompt starts on the loaded path, so it can be edited rather than
	// retyped.
	if string(a.prompt.buf) != orig {
		t.Errorf("prompt started at %q, want the loaded path", a.prompt.buf)
	}
	press(a, tcell.KeyCtrlU)
	typeInto(a, other)
	press(a, tcell.KeyEnter)

	if !a.quit {
		t.Fatal("save-as did not quit")
	}
	if _, err := ruleset.Load(other); err != nil {
		t.Fatalf("the new file was not written: %v", err)
	}
	// The original must be left as it was.
	f, err := ruleset.Load(orig)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Rules) != 1 {
		t.Errorf("the original file was modified: %d rules", len(f.Rules))
	}
}

func TestSavingClearsTheQuestion(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))
	a.addRule(filter.Only, "ERROR", color.RGB{})

	path := filepath.Join(t.TempDir(), "rules.json")
	a.runCommandLine("rules-save " + path)

	quitKey(a)
	if a.confirm != nil {
		t.Fatal("asked again after the rules were saved")
	}
	if !a.quit {
		t.Fatal("did not quit")
	}
}

func TestRulesRoundTripThroughASession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")

	first, _ := newTestApp(t, numbered(90))
	first.addRule(filter.Only, "ERROR", color.RGB{})
	first.addRule(filter.Highlight, "bad", color.RGB{})
	first.applyRules(first.rules.Toggle(1), false) // leave one disabled
	waitFiltered(t, first)

	want := first.viewLen()
	if !first.saveRules(path) {
		t.Fatal("saving failed")
	}

	second, _ := newTestApp(t, numbered(90))
	if err := second.LoadRules(path); err != nil {
		t.Fatal(err)
	}
	waitFiltered(t, second)

	if got := second.viewLen(); got != want {
		t.Errorf("restored view has %d lines, want %d", got, want)
	}
	if second.rules.Len() != 2 {
		t.Fatalf("restored %d rules, want 2", second.rules.Len())
	}

	orig, restored := first.rules.All(), second.rules.All()
	for i := range orig {
		if restored[i].Action != orig[i].Action ||
			restored[i].M.Pattern() != orig[i].M.Pattern() ||
			restored[i].Enabled != orig[i].Enabled ||
			restored[i].Color != orig[i].Color {
			t.Errorf("rule %d came back as %+v", i, restored[i])
		}
	}
}

func TestLoadRulesDuringASession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	if err := os.WriteFile(path, []byte(`{"rules":[{"action":"never","pattern":"INFO"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	a, _ := newTestApp(t, numbered(60))
	a.addRule(filter.Only, "nothing-matches-this", color.RGB{})
	waitFiltered(t, a)

	a.runCommandLine("source " + path)
	waitFiltered(t, a)

	if a.rules.Len() != 1 {
		t.Fatalf("loading left %d rules, want the file's 1", a.rules.Len())
	}
	if a.rules.All()[0].Action != filter.Never {
		t.Errorf("action = %v, want never", a.rules.All()[0].Action)
	}
	if a.viewLen() == 0 {
		t.Error("the loaded rule hid everything")
	}
}

func TestLoadRulesReportsABadFile(t *testing.T) {
	a, _ := newTestApp(t, numbered(20))
	a.runCommandLine("source /definitely/not/here.json")
	if a.msg == "" {
		t.Error("a missing rules file was not reported")
	}
}

func TestSavedRulesUseTheSessionCase(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))
	a.setOption([]string{"case", "sensitive"})
	a.addRule(filter.Only, "error", color.RGB{})

	path := filepath.Join(t.TempDir(), "rules.json")
	if !a.saveRules(path) {
		t.Fatal("saving failed")
	}
	f, err := ruleset.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Case != "sensitive" {
		t.Errorf("case = %q, want sensitive", f.Case)
	}
}

// TestConfigPresetsAreNotUnsavedRules: presets come from the configuration,
// where they are already kept, so quitting an untouched session asks nothing.
func TestConfigPresetsAreNotUnsavedRules(t *testing.T) {
	a, _, _ := newTestAppConf(t, numbered(50), "rule highlight ERROR\n")
	if a.rules.Len() != 1 {
		t.Fatalf("%d rule(s) from the preset, want 1", a.rules.Len())
	}
	if a.rulesDirty() {
		t.Error("a config preset counts as an unsaved rule")
	}
	run(a, "quit")
	if a.confirm != nil || !a.quit {
		t.Errorf("quit with only presets: confirm %+v, quit %v", a.confirm, a.quit)
	}
}
