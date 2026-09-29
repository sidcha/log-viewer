package main

import (
	"runtime/debug"
	"testing"
)

func TestVersionFromBuildInfo(t *testing.T) {
	for _, c := range []struct {
		name     string
		mod      string
		settings map[string]string
		want     string
	}{
		{"tagged release", "v0.2.0", nil, "0.2.0"},
		{"commit after a tag", "v0.2.1-0.20260927191145-fa0dfb0b3d0e", nil, "0.2.0-gfa0dfb0"},
		{"commit after a prerelease", "v1.0.0-rc.1.0.20260927191145-fa0dfb0b3d0e", nil, "1.0.0-rc.1-gfa0dfb0"},
		{"no tags yet", "v0.0.0-20260927191145-fa0dfb0b3d0e", nil, "fa0dfb0"},
		{"edited checkout", "v0.2.1-0.20260927191145-fa0dfb0b3d0e+dirty", nil, "0.2.0-gfa0dfb0-dirty"},
		{"tagged but edited", "v0.2.0+dirty", nil, "0.2.0-dirty"},
		{"no module version, revision known", "(devel)",
			map[string]string{"vcs.revision": "fa0dfb0b3d0e3f23d810f32e14bc02fbef667da5", "vcs.modified": "true"},
			"fa0dfb0-dirty"},
		{"nothing known", "(devel)", nil, "dev"},
	} {
		t.Run(c.name, func(t *testing.T) {
			bi := &debug.BuildInfo{Main: debug.Module{Version: c.mod}}
			for k, v := range c.settings {
				bi.Settings = append(bi.Settings, debug.BuildSetting{Key: k, Value: v})
			}
			if got := versionFrom(bi); got != c.want {
				t.Errorf("versionFrom(%q) = %q, want %q", c.mod, got, c.want)
			}
		})
	}
	if got := versionFrom(nil); got != "dev" {
		t.Errorf("versionFrom(nil) = %q, want dev", got)
	}
}
