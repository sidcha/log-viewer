package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/source"
)

// newPipedApp builds the viewer over real piped input, spooled the way a
// `… | lv` session spools it.
func newPipedApp(t *testing.T, content string) (*App, tcell.SimulationScreen) {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old })

	if _, err := w.WriteString(content); err != nil {
		t.Fatal(err)
	}
	w.Close()

	src, err := source.FromStdin()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { src.Close() })

	deadline := time.Now().Add(5 * time.Second)
	for src.Streaming() {
		if time.Now().After(deadline) {
			t.Fatal("the spool never finished")
		}
		time.Sleep(time.Millisecond)
	}
	return newAppOver(t, src, "(stdin)", content)
}

// newWatchedPipe is the capture case: lv was following the input as it
// arrived, with ask-save auto, which asks about exactly that on the way out.
func newWatchedPipe(t *testing.T, content string) (*App, tcell.SimulationScreen) {
	t.Helper()
	a, scr := newPipedApp(t, content)
	if err := a.cfg.Set("ask-save", "auto"); err != nil {
		t.Fatal(err)
	}
	run(a, "follow-toggle")
	if !a.everFollowed {
		t.Fatal("follow-toggle did not start following")
	}
	return a, scr
}

func quitAndAnswer(t *testing.T, a *App, answers string) {
	t.Helper()
	run(a, "quit")
	for _, r := range answers {
		if a.confirm == nil {
			t.Fatalf("no question to answer with %q", r)
		}
		a.key(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
}

// TestPipedInputIsNotThrownAwaySilently. The spool is unlinked the moment it
// is opened, so quitting is the last chance anything has to keep it.
func TestPipedInputAsksBeforeQuitting(t *testing.T) {
	a, _ := newWatchedPipe(t, numbered(40))

	run(a, "quit")
	if a.confirm == nil {
		t.Fatal("quitting piped input asked nothing")
	}
	if a.quit {
		t.Fatal("quit before the question was answered")
	}

	a.key(tcell.NewEventKey(tcell.KeyRune, 'c', tcell.ModNone)) // cancel
	if a.quit {
		t.Error("cancel quit anyway")
	}
}

func TestPipedInputSavesThenQuits(t *testing.T) {
	content := numbered(40)
	a, _ := newWatchedPipe(t, content)
	path := filepath.Join(t.TempDir(), "capture.log")

	quitAndAnswer(t, a, "y")
	if a.prompt == nil || a.prompt.kind != pSaveStream {
		t.Fatal("save did not open a path prompt")
	}
	if len(a.prompt.buf) == 0 {
		t.Error("no file name was suggested to type over")
	}

	press(a, tcell.KeyCtrlU)
	typeInto(a, path)
	press(a, tcell.KeyEnter)

	// The write runs in the background: quitting before it lands would lose
	// exactly what was being saved.
	if a.quit {
		t.Fatal("quit before the save finished")
	}
	waitSave(t, a)
	if !a.quit {
		t.Fatal("the save finished but the session did not quit")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Errorf("wrote %d bytes, want the %d captured", len(got), len(content))
	}
}

// TestPipedSaveWritesEverythingNotTheFilteredView: the question was about
// losing the input, so answering it with a subset would be a trap.
func TestPipedSaveWritesEverythingNotTheFilteredView(t *testing.T) {
	content := numbered(60)
	a, _ := newWatchedPipe(t, content)
	a.runCommandLine("only ERROR")
	waitFiltered(t, a)
	if a.viewLen() >= a.base.Len() {
		t.Fatal("the filter did not narrow the view")
	}

	path := filepath.Join(t.TempDir(), "capture.log")
	quitAndAnswer(t, a, "ny") // discard the rules, keep the input
	press(a, tcell.KeyCtrlU)
	typeInto(a, path)
	press(a, tcell.KeyEnter)
	waitSave(t, a)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Errorf("wrote %d bytes, want all %d", len(got), len(content))
	}
}

// TestSavingTheWholeStreamStopsTheQuestion, so `:w` then `q` does not ask
// about something already on disk.
func TestSavingTheWholeStreamStopsTheQuestion(t *testing.T) {
	a, _ := newPipedApp(t, numbered(40))
	path := filepath.Join(t.TempDir(), "all.log")

	a.runCommandLine("w " + path)
	waitSave(t, a)
	if !a.streamSaved {
		t.Fatal("a full save was not recorded")
	}

	run(a, "quit")
	if a.confirm != nil {
		t.Errorf("asked anyway: %q", a.confirm.text)
	}
	if !a.quit {
		t.Error("did not quit")
	}
}

// TestSavingAFilteredViewDoesNotCount: what was written is not the input.
func TestSavingAFilteredViewDoesNotCount(t *testing.T) {
	a, _ := newPipedApp(t, numbered(60))
	a.runCommandLine("only ERROR")
	waitFiltered(t, a)

	a.runCommandLine("w " + filepath.Join(t.TempDir(), "some.log"))
	waitSave(t, a)
	if a.streamSaved {
		t.Error("a filtered save was taken for the whole input")
	}
}

// TestPagingAPipeQuitsWithoutAsking. lv has to be usable as git's pager, and a
// pager that stopped to ask a question on the way out of `git log` would be
// intolerable. The output of a command can be produced again by running it;
// what was watched arriving cannot.
func TestPagingAPipeQuitsWithoutAsking(t *testing.T) {
	a, _ := newPipedApp(t, numbered(40))

	run(a, "quit")
	if a.confirm != nil {
		t.Errorf("paging a pipe asked %q", a.confirm.text)
	}
	if !a.quit {
		t.Error("did not quit")
	}
}

// TestFollowingAPipeQuitsWithoutAskingByDefault. lv is a pager first: a
// question on the way out of every `journalctl -f | lv -f` is a toll paid on
// every session to protect the rare one worth keeping. ask-save auto or on
// brings it back.
func TestFollowingAPipeQuitsWithoutAskingByDefault(t *testing.T) {
	a, _ := newPipedApp(t, numbered(40))
	run(a, "follow-toggle")

	run(a, "quit")
	if a.confirm != nil {
		t.Errorf("following a pipe asked %q", a.confirm.text)
	}
	if !a.quit {
		t.Error("did not quit")
	}
}

// TestAskSaveOnAsksEvenWhenPaging, for anyone who wants every pipe protected.
func TestAskSaveOnAsksEvenWhenPaging(t *testing.T) {
	a, _ := newPipedApp(t, numbered(40))
	if err := a.cfg.Set("ask-save", "on"); err != nil {
		t.Fatal(err)
	}

	run(a, "quit")
	if a.confirm == nil {
		t.Fatal("ask-save on did not ask")
	}
}

// TestFollowingIsRememberedAfterItStops: turning follow off to scroll back
// through what was captured does not make the capture worthless.
func TestFollowingIsRememberedAfterItStops(t *testing.T) {
	a, _ := newWatchedPipe(t, numbered(40))
	run(a, "follow-toggle")
	if a.follow {
		t.Fatal("follow is still on")
	}

	run(a, "quit")
	if a.confirm == nil {
		t.Error("a capture stopped being worth asking about the moment follow was turned off")
	}
}

func TestAskSaveOffQuitsStraightAway(t *testing.T) {
	a, _ := newWatchedPipe(t, numbered(40))
	if err := a.cfg.Set("ask-save", "off"); err != nil {
		t.Fatal(err)
	}

	run(a, "quit")
	if a.confirm != nil {
		t.Errorf("asked with ask-save off: %q", a.confirm.text)
	}
	if !a.quit {
		t.Error("did not quit")
	}
}

func TestAskSaveRulesOffQuitsStraightAway(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))
	a.runCommandLine("only ERROR")
	waitFiltered(t, a)
	if !a.rulesDirty() {
		t.Fatal("expected unsaved rules")
	}
	if err := a.cfg.Set("ask-save-rules", "off"); err != nil {
		t.Fatal(err)
	}

	run(a, "quit")
	if a.confirm != nil {
		t.Errorf("asked with ask-save-rules off: %q", a.confirm.text)
	}
	if !a.quit {
		t.Error("did not quit")
	}
}

// TestAFileIsNeverAtRisk: it is already on disk, which is the whole point.
func TestAFileIsNeverAtRisk(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))
	if a.streamAtRisk() {
		t.Error("a file was treated as unsaved input")
	}
	run(a, "quit")
	if !a.quit || a.confirm != nil {
		t.Error("quitting a file asked something")
	}
}

// TestBothQuestionsAreAsked when there is both a rule set and a capture to
// lose, and the session only ends once both are settled.
func TestBothQuestionsAreAsked(t *testing.T) {
	a, _ := newWatchedPipe(t, numbered(40))
	a.runCommandLine("only ERROR")
	waitFiltered(t, a)

	run(a, "quit")
	if a.confirm == nil {
		t.Fatal("no question")
	}
	a.key(tcell.NewEventKey(tcell.KeyRune, 'n', tcell.ModNone)) // discard rules
	if a.quit {
		t.Fatal("quit without asking about the input")
	}
	if a.confirm == nil {
		t.Fatal("the input question was not asked")
	}
	a.key(tcell.NewEventKey(tcell.KeyRune, 'n', tcell.ModNone)) // discard input
	if !a.quit {
		t.Error("did not quit once both were answered")
	}
}

// TestEveryAnswerFitsOnTheRow: a question whose choices are cut off the right
// edge cannot be answered by anyone who has not read the source.
func TestEveryAnswerFitsOnTheRow(t *testing.T) {
	a, scr := newWatchedPipe(t, numbered(40))
	// A rules file with a name long enough to crowd the row out on its own.
	a.rulesPath = filepath.Join(t.TempDir(), strings.Repeat("long-", 12)+"rules.json")
	a.runCommandLine("only ERROR")
	waitFiltered(t, a)

	run(a, "quit")
	a.draw()

	row := rowText(scr, testH-1)
	for _, want := range []string{"(y)", "(n)", "(a)", "(c)"} {
		if !strings.Contains(row, want) {
			t.Errorf("answer %s was cut off: %q", want, row)
		}
	}
	if !strings.Contains(row, "…") {
		t.Errorf("the question was not marked as shortened: %q", row)
	}
}

// waitSave runs the event loop's part until the background write is done.
func waitSave(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for a.busy != nil {
		select {
		case fn := <-a.post:
			fn()
		case <-time.After(time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("the save never finished")
		}
	}
}
