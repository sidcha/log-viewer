package view

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/sidcha/log-viewer/internal/highlight"
	"github.com/sidcha/log-viewer/internal/index"
	"github.com/sidcha/log-viewer/internal/source"
)

// benchMarkers is a dozen markers shaped like the boot-stage set.
func benchMarkers() []*highlight.Marker {
	var ms []*highlight.Marker
	for _, lit := range []string{"U-Boot ", "GNU GRUB", "Booting ", "Loading Linux", "UEFI firmware",
		"BdsDxe:", "NOTICE:  BL", "OpenSBI v", "Starting kernel", "EFI stub: ",
		"Booting Linux on physical CPU", "Welcome to "} {
		ms = append(ms, &highlight.Marker{Lit: []byte(lit), Re: regexp.MustCompile(`^` + regexp.QuoteMeta(lit))})
	}
	return ms
}

func benchFile(b *testing.B) string {
	b.Helper()
	path := filepath.Join(b.TempDir(), "bench.log")
	var sb strings.Builder
	for sb.Len() < 64<<20 {
		fmt.Fprintf(&sb, "[%5d.%06d] usb 1-1: new high-speed USB device number %d using ehci-pci\n", sb.Len()%99999, sb.Len()%999999, sb.Len()%97)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o600); err != nil {
		b.Fatal(err)
	}
	return path
}

func benchIndex(b *testing.B, withDetector bool) {
	path := benchFile(b)
	src, err := source.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	defer src.Close()
	b.SetBytes(src.Size())
	for b.Loop() {
		var obs index.Observer
		if withDetector {
			gen := &highlight.Profile{Name: "generic"}
			det := highlight.NewDetector(highlight.DetectOptions{
				Profiles: []*highlight.Profile{gen}, Markers: benchMarkers(), Generic: gen,
				Lines: 64, Threshold: 30,
			})
			obs = det
			b.Cleanup(det.Close)
		}
		index.NewIndexer(src, index.New(0, 0), index.Options{Observer: obs}).Sync(context.Background())
	}
}

func BenchmarkIndex(b *testing.B)             { benchIndex(b, false) }
func BenchmarkIndexWithDetector(b *testing.B) { benchIndex(b, true) }
