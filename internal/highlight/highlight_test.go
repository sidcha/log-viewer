package highlight

import (
	"regexp"
	"testing"
)

func numberEngine() *Engine {
	e := NewEngine(0, 0)
	e.Profile = &Profile{Name: "t", Rules: []Rule{{Re: regexp.MustCompile(`[0-9]+`)}}}
	return e
}

func TestSyntaxSpansAtReusesTheAnswerForTheSameLine(t *testing.T) {
	e := numberEngine()
	key := LineKey{Gen: 1, Off: 100, Len: 9}
	first := e.SyntaxSpansAt(key, []byte("id 42 ok7"))
	if len(first) != 2 {
		t.Fatalf("spans = %v, want two numbers", first)
	}
	// The same key is the same bytes; the cache must answer without looking.
	again := e.SyntaxSpansAt(key, []byte("no digits"))
	if len(again) != 2 || &again[0] != &first[0] {
		t.Error("the same line was highlighted again instead of reused")
	}
}

func TestSyntaxSpansAtSeesAGrowingOrReplacedLine(t *testing.T) {
	e := numberEngine()
	e.SyntaxSpansAt(LineKey{Gen: 1, Off: 0, Len: 3}, []byte("abc"))
	if got := e.SyntaxSpansAt(LineKey{Gen: 1, Off: 0, Len: 6}, []byte("abc123")); len(got) != 1 {
		t.Errorf("a grown line kept its old spans: %v", got)
	}
	if got := e.SyntaxSpansAt(LineKey{Gen: 2, Off: 0, Len: 3}, []byte("9 9")); len(got) != 2 {
		t.Errorf("a replaced file kept its old spans: %v", got)
	}
}

func TestSyntaxSpansAtStaysBounded(t *testing.T) {
	e := numberEngine()
	for i := 0; i < 3*maxCached; i++ {
		e.SyntaxSpansAt(LineKey{Off: int64(i), Len: 1}, []byte("1"))
	}
	if len(e.cache) > maxCached {
		t.Errorf("cache holds %d lines, cap %d", len(e.cache), maxCached)
	}
}
