package highlight

// Profiler says which profile applies to the line starting at a byte
// offset. Version changes whenever an answer may have, so a cache of spans
// knows to start over.
type Profiler interface {
	ProfileAt(off int64) *Profile
	// Describe says, for a person, which profile the line at off gets and
	// why; "" when there is none.
	Describe(off int64) string
	Version() uint64
}

// Fixed applies one profile to every line. A nil P turns syntax colouring
// off.
type Fixed struct {
	P   *Profile
	Why string // how it was chosen, e.g. "forced"; may be empty
}

func (f Fixed) ProfileAt(int64) *Profile { return f.P }
func (f Fixed) Version() uint64          { return 0 }

func (f Fixed) Describe(int64) string {
	if f.P == nil {
		return ""
	}
	if f.Why == "" {
		return f.P.Name
	}
	return f.P.Name + " (" + f.Why + ")"
}
