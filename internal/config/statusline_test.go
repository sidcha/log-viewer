package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseStatusSplitsTextAndFields(t *testing.T) {
	got, err := ParseStatus(" {file}  {{x}} {line}/{lines}")
	if err != nil {
		t.Fatal(err)
	}
	want := []StatusPart{
		{Text: " "},
		{Field: "file"},
		{Text: "  {x}} "},
		{Field: "line"},
		{Text: "/"},
		{Field: "lines"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseStatus = %#v\nwant %#v", got, want)
	}
}

// A misspelled field would otherwise print nothing, and nothing says why.
func TestStatusTemplatesAreCheckedWhenSet(t *testing.T) {
	c, err := Load(LoadOptions{NoUser: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ value, want string }{
		{"{file} {nope}", `unknown field "nope"`},
		{"{file", "unclosed {"},
	} {
		for _, name := range []string{"status-left", "status-right"} {
			err := c.Set(name, tc.value)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("set %s %q: err = %v, want %q", name, tc.value, err, tc.want)
			}
		}
	}
	if err := c.Set("status-right", "{percent}"); err != nil {
		t.Errorf("a known field was rejected: %v", err)
	}
}

func TestDefaultStatusTemplatesParse(t *testing.T) {
	for _, name := range []string{"status-left", "status-right"} {
		s, _ := LookupSetting(name)
		if _, err := ParseStatus(s.Def); err != nil {
			t.Errorf("default %s: %v", name, err)
		}
	}
}
