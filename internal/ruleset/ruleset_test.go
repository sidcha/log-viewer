package ruleset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sidcha/log-viewer/internal/filter"
)

func mustSet(t *testing.T, rules ...filter.Rule) *filter.Set {
	t.Helper()
	s := filter.NewSet()
	for _, r := range rules {
		s = s.Add(r)
	}
	return s
}

func rule(t *testing.T, a filter.Action, pat string, enabled bool) filter.Rule {
	t.Helper()
	m, err := filter.Compile(pat, filter.Sensitive)
	if err != nil {
		t.Fatalf("Compile(%q): %v", pat, err)
	}
	return filter.Rule{Action: a, M: m, Enabled: enabled}
}

func TestRoundTrip(t *testing.T) {
	set := mustSet(t,
		rule(t, filter.Only, "ERROR", true),
		rule(t, filter.Never, "healthz", false),
		rule(t, filter.Highlight, `OOM|oom-killer`, true),
	)

	f := From(set, filter.Sensitive, "timeout", true)
	path := filepath.Join(t.TempDir(), "rules.json")
	if err := f.Save(path); err != nil {
		t.Fatal(err)
	}

	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, cs, err := back.Set(filter.Smart)
	if err != nil {
		t.Fatal(err)
	}

	if cs != filter.Sensitive {
		t.Errorf("case came back as %v, want sensitive", cs)
	}
	if got.Len() != 3 {
		t.Fatalf("got %d rules, want 3", got.Len())
	}

	orig, loaded := set.All(), got.All()
	for i := range orig {
		if loaded[i].Action != orig[i].Action {
			t.Errorf("rule %d action = %v, want %v", i, loaded[i].Action, orig[i].Action)
		}
		if loaded[i].M.Pattern() != orig[i].M.Pattern() {
			t.Errorf("rule %d pattern = %q, want %q", i, loaded[i].M.Pattern(), orig[i].M.Pattern())
		}
		if loaded[i].Enabled != orig[i].Enabled {
			t.Errorf("rule %d enabled = %v, want %v", i, loaded[i].Enabled, orig[i].Enabled)
		}
		if loaded[i].Color != orig[i].Color {
			t.Errorf("rule %d colour = %+v, want %+v", i, loaded[i].Color, orig[i].Color)
		}
	}

	if back.Search == nil || back.Search.Pattern != "timeout" || !back.Search.Backward {
		t.Errorf("search came back as %+v", back.Search)
	}
}

// TestCaseIsPreserved is the point of storing it: a set built under sensitive
// matching must not quietly widen when reloaded somewhere set to smart.
func TestCaseIsPreserved(t *testing.T) {
	set := mustSet(t, rule(t, filter.Only, "error", true))
	f := From(set, filter.Sensitive, "", false)

	path := filepath.Join(t.TempDir(), "rules.json")
	if err := f.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, cs, err := back.Set(filter.Smart)
	if err != nil {
		t.Fatal(err)
	}
	if cs != filter.Sensitive {
		t.Fatalf("case = %v, want sensitive", cs)
	}
	if got.Pass([]byte("an ERROR happened")) {
		t.Error("the reloaded rule matched case-insensitively")
	}
	if !got.Pass([]byte("an error happened")) {
		t.Error("the reloaded rule stopped matching its own case")
	}
}

func TestMinimalHandWrittenFile(t *testing.T) {
	// Everything except action and pattern should be optional.
	src := `{"rules":[{"action":"only","pattern":"ERROR"},{"action":"never","pattern":"noise"}]}`
	path := filepath.Join(t.TempDir(), "rules.json")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	set, cs, err := f.Set(filter.Smart)
	if err != nil {
		t.Fatal(err)
	}
	if cs != filter.Smart {
		t.Errorf("case = %v, want the fallback smart", cs)
	}
	if set.Len() != 2 {
		t.Fatalf("got %d rules", set.Len())
	}
	for i, r := range set.All() {
		if !r.Enabled {
			t.Errorf("rule %d should default to enabled", i)
		}
		if r.Color == (filter.Color{}) {
			t.Errorf("rule %d should have been assigned a colour", i)
		}
	}
}

func TestActionAliases(t *testing.T) {
	src := `{"rules":[{"action":"keep","pattern":"a"},{"action":"drop","pattern":"b"},{"action":"mark","pattern":"c"}]}`
	var f File
	if err := json.Unmarshal([]byte(src), &f); err != nil {
		t.Fatal(err)
	}
	set, _, err := f.Set(filter.Smart)
	if err != nil {
		t.Fatal(err)
	}
	want := []filter.Action{filter.Only, filter.Never, filter.Highlight}
	for i, r := range set.All() {
		if r.Action != want[i] {
			t.Errorf("rule %d action = %v, want %v", i, r.Action, want[i])
		}
	}
}

func TestLoadErrors(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"not json":       `{`,
		"bad action":     `{"rules":[{"action":"sideways","pattern":"a"}]}`,
		"empty pattern":  `{"rules":[{"action":"only","pattern":""}]}`,
		"bad regex":      `{"rules":[{"action":"only","pattern":"ERR("}]}`,
		"bad colour":     `{"rules":[{"action":"only","pattern":"a","color":"red"}]}`,
		"short colour":   `{"rules":[{"action":"only","pattern":"a","color":"#fff"}]}`,
		"unknown case":   `{"case":"whatever","rules":[]}`,
		"future version": `{"version":99,"rules":[]}`,
	}

	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, "x.json")
			if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			f, err := Load(path)
			if err != nil {
				return // rejected at load, which is fine
			}
			if _, _, err := f.Set(filter.Smart); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}

	if _, err := Load(filepath.Join(dir, "does-not-exist.json")); err == nil {
		t.Error("loading a missing file should fail")
	}
}

// TestFutureVersionIsRefused matters because silently ignoring fields written
// by a newer lv would restore a different set than the one that was saved.
func TestFutureVersionIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"rules":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("a newer format should be refused")
	}
	if !strings.Contains(err.Error(), "newer lv") {
		t.Errorf("error = %v, want it to explain why", err)
	}
}

func TestDigestDetectsChange(t *testing.T) {
	set := mustSet(t, rule(t, filter.Only, "ERROR", true))
	a := From(set, filter.Smart, "", false)

	if a.Digest() != From(set, filter.Smart, "", false).Digest() {
		t.Error("the same set produced different digests")
	}

	bigger := set.Add(rule(t, filter.Never, "noise", true))
	if a.Digest() == From(bigger, filter.Smart, "", false).Digest() {
		t.Error("adding a rule did not change the digest")
	}
	if a.Digest() == From(set, filter.Smart, "timeout", false).Digest() {
		t.Error("adding a search did not change the digest")
	}
	if a.Digest() == From(set.Toggle(0), filter.Smart, "", false).Digest() {
		t.Error("disabling a rule did not change the digest")
	}
}

func TestSaveIsAtomicAndCreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deeper", "rules.json")

	f := From(mustSet(t, rule(t, filter.Only, "ERROR", true)), filter.Smart, "", false)
	if err := f.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file was not written: %v", err)
	}

	// Overwriting must leave no stray temporaries behind.
	if err := f.Save(path); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("left a temporary behind: %s", e.Name())
		}
	}
}

func TestEmpty(t *testing.T) {
	if !(&File{}).Empty() {
		t.Error("a file with nothing in it should be empty")
	}
	if (*File)(nil).Empty() != true {
		t.Error("a nil file should be empty")
	}
	f := From(mustSet(t, rule(t, filter.Only, "x", true)), filter.Smart, "", false)
	if f.Empty() {
		t.Error("a file with a rule should not be empty")
	}
	if (&File{Search: &Search{Pattern: "x"}}).Empty() {
		t.Error("a file with only a search should not be empty")
	}
}

func TestSavedFileIsReadable(t *testing.T) {
	// The format is meant to be edited by hand and reviewed in a diff, so the
	// output has to be indented and stable.
	f := From(mustSet(t, rule(t, filter.Only, "ERROR", true)), filter.Smart, "", false)
	path := filepath.Join(t.TempDir(), "rules.json")
	if err := f.Save(path); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "\n  ") {
		t.Errorf("output is not indented:\n%s", text)
	}
	if !strings.HasSuffix(text, "\n") {
		t.Error("output does not end with a newline")
	}
	if !strings.Contains(text, `"action": "only"`) {
		t.Errorf("output does not name the action:\n%s", text)
	}
}
