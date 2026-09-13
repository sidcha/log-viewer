package view

// posCacheSize is how many recently read positions a view remembers: enough
// for every row of a tall screen, the rows either side of it, and the lines
// motions read on the way.
const posCacheSize = 256

// position is where a view line starts: its view index, byte offset and
// 0-based source line.
type position struct {
	idx, off, src int64
}

// posCache remembers where recently read view lines start, so redrawing a
// screen, or moving a line up or down, does not walk from a checkpoint again.
//
// Entries are tagged with the index generation they were found under; a
// truncated or rotated file bumps it and the whole cache lapses. The cache
// belongs to the goroutine that calls At.
type posCache struct {
	gen  uint64
	ents [posCacheSize]position
	n    int
	next int
}

func (c *posCache) put(gen uint64, p position) {
	if gen != c.gen {
		c.gen, c.n, c.next = gen, 0, 0
	}
	for i := 0; i < c.n; i++ {
		if c.ents[i].idx == p.idx {
			// The same line can be remembered from two places: where it was
			// read, and just after the line before it. In a filtered view the
			// second may be a long way short of it; keep the closer one.
			if p.off > c.ents[i].off {
				c.ents[i] = p
			}
			return
		}
	}
	c.ents[c.next] = p
	c.next = (c.next + 1) % posCacheSize
	if c.n < posCacheSize {
		c.n++
	}
}

// nearest returns the remembered position closest to view line i without
// passing it.
func (c *posCache) nearest(gen uint64, i int64) (position, bool) {
	if gen != c.gen {
		return position{}, false
	}
	var (
		best  position
		found bool
	)
	for k := 0; k < c.n; k++ {
		p := c.ents[k]
		if p.idx <= i && (!found || p.idx > best.idx) {
			best, found = p, true
		}
	}
	return best, found
}
