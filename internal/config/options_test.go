package config

import (
	"reflect"
	"testing"
	"time"

	"github.com/sidcha/log-viewer/internal/filter"
)

// TestOptionsCoverEverySetting: a setting with no field would be accepted and
// never read, and a field with no setting would always be zero.
func TestOptionsCoverEverySetting(t *testing.T) {
	fields := map[string]reflect.Type{}
	ty := reflect.TypeOf(Options{})
	for i := 0; i < ty.NumField(); i++ {
		name := ty.Field(i).Tag.Get("setting")
		if _, ok := LookupSetting(name); !ok {
			t.Errorf("Options.%s names %q, which is not a setting", ty.Field(i).Name, name)
		}
		fields[name] = ty.Field(i).Type
	}
	want := map[kind][]reflect.Type{
		kBool: {reflect.TypeOf(false)},
		kInt:  {reflect.TypeOf(0)},
		kStr:  {reflect.TypeOf("")},
		kEnum: {reflect.TypeOf(""), reflect.TypeOf(filter.Case(0))},
		kDur:  {reflect.TypeOf(time.Duration(0))},
	}
	for _, s := range Settings {
		ft, ok := fields[s.Name]
		if !ok {
			t.Errorf("setting %q has no field in Options", s.Name)
			continue
		}
		fits := false
		for _, w := range want[s.Kind] {
			fits = fits || ft == w
		}
		if !fits {
			t.Errorf("setting %q is held as %s", s.Name, ft)
		}
	}
}

func TestOptionsFollowTheSettings(t *testing.T) {
	c, err := Load(LoadOptions{NoUser: true})
	if err != nil {
		t.Fatal(err)
	}
	v := c.Version()
	for _, kv := range [][2]string{{"tab-width", "3"}, {"wrap", "on"}, {"case", "sensitive"}, {"message-timeout", "9s"}, {"gutter", "|"}} {
		if err := c.Set(kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	if c.Version() == v {
		t.Error("changing settings did not change the version")
	}
	o := c.Options()
	if o.TabWidth != 3 || !o.Wrap || o.Case != filter.Sensitive || o.MessageTimeout != 9*time.Second || o.Gutter != "|" {
		t.Errorf("Options = %+v", o)
	}
}

func TestDetectSettingsHaveTheirRange(t *testing.T) {
	c := New()
	o := c.Options()
	if o.DetectLines != 64 || o.DetectThreshold != 30 || o.DetectWait != 2*time.Second {
		t.Errorf("defaults = %d, %d, %v", o.DetectLines, o.DetectThreshold, o.DetectWait)
	}
	for _, bad := range [][2]string{{"detect-lines", "0"}, {"detect-threshold", "0"}, {"detect-threshold", "101"}, {"detect-wait", "0"}, {"detect-wait", "-1s"}} {
		if err := c.SetFrom(bad[0], bad[1], "<t>"); err == nil {
			t.Errorf("set %s %s was accepted", bad[0], bad[1])
		}
	}
}
