// Package checkpoint is the sparse position table behind every addressable
// sequence of lines.
//
// A dense table of line offsets is unaffordable: a 100 GB log holds on the
// order of 10^9 lines. Instead a Table records a position only now and then,
// and reaching an arbitrary item means looking up the nearest entry at or
// before it and scanning forward from there.
//
// Entries are recorded on two axes. Every keyStride-th item gets one, which
// bounds the forward scan in items; and whenever byteStride bytes pass without
// an entry, the position reached gets one, which bounds it in bytes. Both
// matter for a filtered view: with matches dense the first bound is the
// tighter, and with matches rare it is the second -- without it, reaching the
// next match after a gigabyte of non-matching lines would mean reading that
// gigabyte every time.
//
// Two neighbouring entries with the same key have no item between them. A
// scan for a narrower predicate than the one that built a table can use that
// to jump over whole runs of the file.
//
// The table is capped. When it fills, every second entry is dropped and both
// strides double, so memory is O(1) in the size of the file.
package checkpoint

import "sort"

// Entry is one recorded position.
type Entry struct {
	// Key is the number of items -- lines, or matching lines -- before Off.
	// When an item starts at Off it is item number Key.
	Key int64
	// Off is the byte offset of a line start.
	Off int64
	// Src is the number of source lines before Off.
	Src int64
}

// Table is a sparse, ordered set of entries. It is not safe for concurrent
// use; owners guard it with their own lock.
type Table struct {
	keyStride  int64
	byteStride int64
	max        int
	trackSrc   bool

	keys, offs, srcs []int64
	initKey          int64
	initByte         int64
}

// New returns an empty table. byteStride zero disables the byte axis.
// trackSrc records source line numbers; a table whose items are the source
// lines themselves does not need them, since Src equals Key.
func New(keyStride, byteStride int64, max int, trackSrc bool) *Table {
	if keyStride <= 0 {
		keyStride = 1
	}
	if max < 4 {
		// Below this the table would collapse on almost every append and the
		// strides would run away.
		max = 4
	}
	return &Table{
		keyStride:  keyStride,
		byteStride: byteStride,
		max:        max,
		trackSrc:   trackSrc,
		initKey:    keyStride,
		initByte:   byteStride,
	}
}

// Item notes that item number key starts at off, after src source lines.
func (t *Table) Item(key, off, src int64) {
	if key%t.keyStride == 0 || t.farFromLast(off) {
		t.record(key, off, src)
	}
}

// Pass notes a line start at off that is not an item, with key items before
// it. It is recorded only when the byte axis asks for it.
func (t *Table) Pass(key, off, src int64) {
	if t.farFromLast(off) {
		t.record(key, off, src)
	}
}

// ByteStride is the current byte-axis stride, zero when it is disabled. A
// scan can use it to decide which non-item positions are worth offering.
func (t *Table) ByteStride() int64 { return t.byteStride }

// KeyStride is the current item-axis stride.
func (t *Table) KeyStride() int64 { return t.keyStride }

// Len is the number of entries held.
func (t *Table) Len() int { return len(t.offs) }

// Reset empties the table and restores the initial strides.
func (t *Table) Reset() {
	t.keys, t.offs, t.srcs = t.keys[:0], t.offs[:0], t.srcs[:0]
	t.keyStride, t.byteStride = t.initKey, t.initByte
}

func (t *Table) farFromLast(off int64) bool {
	if len(t.offs) == 0 {
		return true
	}
	return t.byteStride > 0 && off-t.offs[len(t.offs)-1] >= t.byteStride
}

func (t *Table) record(key, off, src int64) {
	if n := len(t.offs); n > 0 && t.offs[n-1] >= off {
		return // already have this position
	}
	t.keys = append(t.keys, key)
	t.offs = append(t.offs, off)
	if t.trackSrc {
		t.srcs = append(t.srcs, src)
	}
	if len(t.offs) >= t.max {
		t.collapse()
	}
}

// collapse halves the table and doubles both strides. Entries that were at
// multiples of the key stride stay at multiples of the new one, and the gap
// between any two remaining neighbours at most doubles.
func (t *Table) collapse() {
	n := 0
	for i := 0; i < len(t.offs); i += 2 {
		t.keys[n], t.offs[n] = t.keys[i], t.offs[i]
		if t.trackSrc {
			t.srcs[n] = t.srcs[i]
		}
		n++
	}
	t.keys, t.offs = t.keys[:n], t.offs[:n]
	if t.trackSrc {
		t.srcs = t.srcs[:n]
	}
	t.keyStride *= 2
	t.byteStride *= 2
}

func (t *Table) at(i int) Entry {
	e := Entry{Key: t.keys[i], Off: t.offs[i], Src: t.keys[i]}
	if t.trackSrc {
		e.Src = t.srcs[i]
	}
	return e
}

// Lookup returns the entry nearest to item key without passing it: the last
// one with at most key items before it. Scanning forward from its offset, the
// first item found is item number Entry.Key.
func (t *Table) Lookup(key int64) (Entry, bool) {
	i := sort.Search(len(t.keys), func(i int) bool { return t.keys[i] > key }) - 1
	if i < 0 {
		return Entry{}, false
	}
	return t.at(i), true
}

// LookupSrc returns the last entry at or before source line src.
func (t *Table) LookupSrc(src int64) (Entry, bool) {
	s := t.srcs
	if !t.trackSrc {
		s = t.keys
	}
	i := sort.Search(len(s), func(i int) bool { return s[i] > src }) - 1
	if i < 0 {
		return Entry{}, false
	}
	return t.at(i), true
}

// Entries returns a copy of every entry, in order, for a reader that walks
// them alongside a scan rather than looking them up one at a time.
func (t *Table) Entries() []Entry {
	out := make([]Entry, len(t.offs))
	for i := range out {
		out[i] = t.at(i)
	}
	return out
}
