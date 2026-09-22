package highlight

import (
	"math/rand"
	"regexp"
	"strings"
	"testing"
)

func litMarkers(lits ...string) []*Marker {
	var ms []*Marker
	for _, l := range lits {
		ms = append(ms, &Marker{Lit: []byte(l)})
	}
	return ms
}

func markerName(m *Marker) string {
	if m == nil {
		return "none"
	}
	return string(m.Lit)
}

func TestMarkerScanFindsWhatFindMarkerFinds(t *testing.T) {
	boot := litMarkers("U-Boot ", "Booting ", "BdsDxe:", "OpenSBI v", "Welcome to ")
	cases := []struct {
		name string
		ms   []*Marker
		text string
		want string
	}{
		{"no marker", boot, "[ 1.0] usb 1-1: new high-speed USB device", "none"},
		{"shared anchor, later literal", boot, "x BdsDxe: loading", "BdsDxe:"},
		{"at the start", boot, "Booting now", "Booting "},
		{"ending the text", boot, "hello Welcome to ", "Welcome to "},
		{"cut short by the end", boot, "hello Welcome to", "none"},
		{"overlapping candidates", boot, "BBBooting x", "Booting "},
		{"anchor before the literal could start", boot, "Boot ", "none"},
		{"config order beats position", boot, "Booting U-Boot 2024", "U-Boot "},
		{"repeated anchor byte in a literal", litMarkers("aXbX"), "XaXbXaXbX", "aXbX"},
		{"empty literal holds everywhere", litMarkers("zzz", ""), "anything", ""},
		{"regex that fails falls through", []*Marker{
			{Lit: []byte("U-Boot "), Re: regexp.MustCompile(`^U-Boot [0-9]`)},
			{Lit: []byte("Boot")},
		}, "see U-Boot x", "Boot"},
	}
	for _, c := range cases {
		s := newMarkerScan(c.ms)
		got := s.find([]byte(c.text))
		if markerName(got) != c.want || got != findMarker(c.ms, []byte(c.text)) {
			t.Errorf("%s: find(%q) = %s, want %s", c.name, c.text, markerName(got), c.want)
		}
	}
}

func TestMarkerScanAgreesWithFindMarkerOnRandomText(t *testing.T) {
	ms := litMarkers("U-Boot ", "GNU GRUB", "Booting ", "Loading Linux", "UEFI firmware",
		"BdsDxe:", "NOTICE:  BL", "OpenSBI v", "Starting kernel", "EFI stub: ", "Welcome to ", "x")
	s := newMarkerScan(ms)
	parts := []string{"U-", "Boot", " ", "GNU", "GRUB", "Bo", "oting", "Linux", "BdsDxe", ":", "NOTICE",
		"BL", "EFI", "stub", "Welcome", "to", "usb", "USB", "[ 1.0]", "Starting", "kernel"}
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 20000; i++ {
		var b strings.Builder
		for n := r.Intn(12); n > 0; n-- {
			b.WriteString(parts[r.Intn(len(parts))])
		}
		text := []byte(b.String())
		if got, want := s.find(text), findMarker(ms, text); got != want {
			t.Fatalf("find(%q) = %s, want %s", text, markerName(got), markerName(want))
		}
	}
}

func TestAnchorsAreSharedAndRare(t *testing.T) {
	anchors := chooseAnchors([][]byte{[]byte("U-Boot "), []byte("Booting "), []byte("BdsDxe:"), []byte("Starting kernel")})
	var got []string
	for _, a := range anchors {
		got = append(got, string(a.b))
	}
	if strings.Join(got, "") != "BS" {
		t.Errorf("anchors = %q, want B shared by three literals, then S", got)
	}
}
