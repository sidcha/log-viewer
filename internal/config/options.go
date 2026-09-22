package config

import (
	"fmt"
	"reflect"
	"time"

	"github.com/sidcha/log-viewer/internal/filter"
)

// Options is every setting, parsed into its Go type.
//
// Code reads settings from here rather than by name, so a value is parsed
// once per change instead of once per use, and a typo in a setting name is a
// compile error. Each field names its setting in a tag; a test checks the
// tags and the Settings table agree, so adding a setting is one row there
// and one field here.
type Options struct {
	Follow           bool          `setting:"follow"`
	Wrap             bool          `setting:"wrap"`
	LineNumbers      bool          `setting:"line-numbers"`
	ANSI             bool          `setting:"ansi"`
	StatusLine       string        `setting:"status-line"`
	AltScreen        bool          `setting:"alt-screen"`
	QuitIfOneScreen  bool          `setting:"quit-if-one-screen"`
	AskSave          string        `setting:"ask-save"`
	AskSaveRules     bool          `setting:"ask-save-rules"`
	Case             filter.Case   `setting:"case"`
	TabWidth         int           `setting:"tab-width"`
	ScrollStep       int           `setting:"scroll-step"`
	HScrollStep      int           `setting:"h-scroll-step"`
	PageOverlap      int           `setting:"page-overlap"`
	WatchBackstop    time.Duration `setting:"watch-backstop"`
	WatchFallback    time.Duration `setting:"watch-fallback"`
	ScanPoll         time.Duration `setting:"scan-poll"`
	BlockSize        int           `setting:"block-size"`
	MaxLineBytes     int           `setting:"max-line-bytes"`
	MaxScanBytes     int           `setting:"max-scan-bytes"`
	IndexStride      int           `setting:"index-stride"`
	MaxCheckpoints   int           `setting:"max-checkpoints"`
	PageCacheAdvise  string        `setting:"page-cache-advise"`
	Profile          string        `setting:"profile"`
	DetectLines      int           `setting:"detect-lines"`
	DetectThreshold  int           `setting:"detect-threshold"`
	DetectWait       time.Duration `setting:"detect-wait"`
	Mouse            bool          `setting:"mouse"`
	Gutter           string        `setting:"gutter"`
	MessageTimeout   time.Duration `setting:"message-timeout"`
	ScrollOff        int           `setting:"scroll-off"`
	CursorLine       bool          `setting:"cursor-line"`
	ClipboardCommand string        `setting:"clipboard-command"`
	StopProducer     bool          `setting:"stop-producer"`
	Background       string        `setting:"background"`
	QueryTimeout     time.Duration `setting:"query-timeout"`
	CursorLineTint   int           `setting:"cursor-line-tint"`
	OneScreenWait    time.Duration `setting:"one-screen-wait"`
	RegexTimeout     time.Duration `setting:"regex-timeout"`
	FilterCkBytes    int           `setting:"filter-checkpoint-bytes"`
	MaxHighlights    int           `setting:"max-highlights"`
	HistorySize      int           `setting:"history-size"`
	OSC52MaxBytes    int           `setting:"osc52-max-bytes"`
	IdleRelease      time.Duration `setting:"idle-release"`
}

// Version changes whenever a setting does, so a holder of Options can tell
// when to ask for fresh ones.
func (c *Config) Version() uint64 { return c.version }

// Options resolves every setting into its typed form.
func (c *Config) Options() Options {
	var o Options
	v := reflect.ValueOf(&o).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		name := t.Field(i).Tag.Get("setting")
		f := v.Field(i)
		switch f.Interface().(type) {
		case bool:
			f.SetBool(c.Bool(name))
		case int:
			f.SetInt(int64(c.Int(name)))
		case string:
			f.SetString(c.Str(name))
		case time.Duration:
			f.SetInt(int64(c.Dur(name)))
		case filter.Case:
			cs, _ := filter.ParseCase(c.Str(name))
			f.Set(reflect.ValueOf(cs))
		default:
			panic(fmt.Sprintf("config: no conversion for option %s of type %s", name, f.Type()))
		}
	}
	return o
}
