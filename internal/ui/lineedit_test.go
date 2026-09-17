package ui

import "testing"

func TestLineEditOperations(t *testing.T) {
	e := newLineEdit(nil)
	for _, r := range "hello world" {
		e.insert(r)
	}
	e.left()
	e.left()
	e.insert('X')
	if got := e.text(); got != "hello worXld" {
		t.Fatalf("after insert: %q", got)
	}
	e.deleteWord()
	if got := e.text(); got != "hello ld" || e.cur != 6 {
		t.Fatalf("after delete-word: %q cur %d", got, e.cur)
	}
	e.killEnd()
	if got := e.text(); got != "hello " {
		t.Fatalf("after kill-end: %q", got)
	}
	if e.del() {
		t.Error("delete at the end changed something")
	}
	e.home()
	if e.backspace() {
		t.Error("backspace at the start changed something")
	}
	e.del()
	if got := e.text(); got != "ello " {
		t.Fatalf("after delete: %q", got)
	}
	e.clear()
	if e.text() != "" || e.cur != 0 {
		t.Fatalf("after clear: %q cur %d", e.text(), e.cur)
	}
}

func TestLineEditHistoryKeepsTheLiveBuffer(t *testing.T) {
	hist := []string{"one", "two"}
	e := newLineEdit(&hist)
	e.set("draft")
	e.history(-1)
	e.history(-1)
	if e.text() != "one" {
		t.Fatalf("two steps back = %q", e.text())
	}
	e.history(-1)
	if e.text() != "one" {
		t.Errorf("stepping past the oldest = %q", e.text())
	}
	e.history(1)
	e.history(1)
	if e.text() != "draft" {
		t.Errorf("back to the live buffer = %q, want the draft", e.text())
	}
}
