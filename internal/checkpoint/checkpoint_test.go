package checkpoint

import "testing"

// A file of 1000 ten-byte lines where every 100th is an item.
func sparse(t *Table) {
	var key int64
	for line := int64(0); line < 1000; line++ {
		off := line * 10
		if line%100 == 0 {
			t.Item(key, off, line)
			key++
		} else {
			t.Pass(key, off, line)
		}
	}
}

func TestLookupNeverPassesTheItem(t *testing.T) {
	tb := New(4, 0, 1<<10, false)
	for k := int64(0); k < 100; k++ {
		tb.Item(k, k*10, k)
	}
	for k := int64(0); k < 100; k++ {
		e, ok := tb.Lookup(k)
		if !ok || e.Key > k || k-e.Key >= 4 || e.Off != e.Key*10 {
			t.Fatalf("Lookup(%d) = %+v, %v", k, e, ok)
		}
	}
}

func TestByteAxisBoundsTheWalkBetweenRareItems(t *testing.T) {
	tb := New(1024, 50, 1<<10, true)
	sparse(tb)
	for k := int64(0); k < 10; k++ {
		e, ok := tb.Lookup(k)
		if !ok {
			t.Fatalf("Lookup(%d) found nothing", k)
		}
		itemOff := k * 100 * 10
		if e.Off > itemOff || itemOff-e.Off > 50 {
			t.Fatalf("Lookup(%d) = %+v; item %d is at %d, more than a byte stride on", k, e, k, itemOff)
		}
		if e.Src*10 != e.Off {
			t.Fatalf("Lookup(%d) = %+v; source line does not match offset", k, e)
		}
	}
}

func TestLookupSrc(t *testing.T) {
	tb := New(1024, 50, 1<<10, true)
	sparse(tb)
	for src := int64(0); src < 1000; src++ {
		e, ok := tb.LookupSrc(src)
		if !ok || e.Src > src || src-e.Src > 5 {
			t.Fatalf("LookupSrc(%d) = %+v, %v", src, e, ok)
		}
		if want := (e.Src + 99) / 100; e.Key != want {
			t.Fatalf("LookupSrc(%d) key = %d, want %d items before line %d", src, e.Key, want, e.Src)
		}
	}
}

func TestCollapseKeepsTheCapAndTheBounds(t *testing.T) {
	tb := New(2, 0, 8, false)
	for k := int64(0); k < 1000; k++ {
		tb.Item(k, k*10, k)
	}
	if tb.Len() >= 8 {
		t.Fatalf("Len = %d, cap not held", tb.Len())
	}
	for k := int64(0); k < 1000; k++ {
		e, _ := tb.Lookup(k)
		if e.Key > k || k-e.Key >= tb.KeyStride() || e.Off != e.Key*10 {
			t.Fatalf("Lookup(%d) = %+v with stride %d", k, e, tb.KeyStride())
		}
	}
}

func TestEntriesAreInOrder(t *testing.T) {
	tb := New(1024, 50, 1<<10, true)
	sparse(tb)
	ents := tb.Entries()
	if len(ents) != tb.Len() {
		t.Fatalf("Entries returned %d of %d", len(ents), tb.Len())
	}
	for i := 1; i < len(ents); i++ {
		if ents[i].Off <= ents[i-1].Off || ents[i].Key < ents[i-1].Key || ents[i].Src < ents[i-1].Src {
			t.Fatalf("entries %d and %d out of order: %+v %+v", i-1, i, ents[i-1], ents[i])
		}
	}
}

func TestResetRestoresStrides(t *testing.T) {
	tb := New(2, 16, 4, false)
	for k := int64(0); k < 100; k++ {
		tb.Item(k, k*10, k)
	}
	tb.Reset()
	if tb.Len() != 0 || tb.KeyStride() != 2 || tb.ByteStride() != 16 {
		t.Fatalf("after Reset: len %d, strides %d/%d", tb.Len(), tb.KeyStride(), tb.ByteStride())
	}
}
