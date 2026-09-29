package main

import (
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
)

// pseudoVersion matches the version Go records for an untagged commit:
// v0.0.0-20260927191145-fa0dfb0b3d0e, or v0.2.1-0.20260927191145-… after v0.2.0.
var pseudoVersion = regexp.MustCompile(`^(.*?)[-.]\d{14}-([0-9a-f]{12})$`)

// versionString is what --version prints. make and nix stamp version; a
// plain `go install` does not, but Go records which commit it built.
func versionString() string {
	if version != "dev" {
		return version
	}
	bi, _ := debug.ReadBuildInfo()
	return versionFrom(bi)
}

// versionFrom reads the build's module version the way `git describe
// --always` would show it: the last tag, the commit when it is not the
// tagged one, and only the commit when there is no tag yet.
func versionFrom(bi *debug.BuildInfo) string {
	if bi == nil {
		return "dev"
	}
	v, dirty := strings.CutSuffix(bi.Main.Version, "+dirty")
	if v != "" && v != "(devel)" {
		v = strings.TrimPrefix(v, "v")
		if m := pseudoVersion.FindStringSubmatch(v); m != nil {
			v = m[2][:7]
			if base := pseudoBase(m[1]); base != "0.0.0" {
				v = base + "-g" + v
			}
		}
		return withDirty(v, dirty)
	}

	var rev string
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if len(rev) < 7 {
		return "dev"
	}
	return withDirty(rev[:7], dirty)
}

// pseudoBase recovers the tag a pseudo-version was counted from. Go writes
// the commit after v0.2.0 as v0.2.1-0.…, and after v1.0.0-rc.1 as
// v1.0.0-rc.1.0.…; with no tag at all the prefix is 0.0.0.
func pseudoBase(prefix string) string {
	if p, ok := strings.CutSuffix(prefix, "-0"); ok {
		i := strings.LastIndexByte(p, '.')
		if n, err := strconv.Atoi(p[i+1:]); err == nil && n > 0 {
			return p[:i+1] + strconv.Itoa(n-1)
		}
		return p
	}
	if p, ok := strings.CutSuffix(prefix, ".0"); ok && strings.Contains(p, "-") {
		return p
	}
	return prefix
}

func withDirty(v string, dirty bool) string {
	if dirty {
		return v + "-dirty"
	}
	return v
}
