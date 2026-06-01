package terminal

// RingBuffer is a circular buffer that stores recent output entries
// for replay on WebSocket reconnection.
type RingBuffer struct {
	entries []OutputEntry
	cap     int
	head    int // oldest entry index
	tail    int // next write position
	count   int // number of entries
	minSeq  uint64 // sequence number of the oldest entry
}

// NewRingBuffer creates a ring buffer with the given capacity (in entries).
func NewRingBuffer(cap int) *RingBuffer {
	if cap <= 0 {
		cap = 1024
	}
	return &RingBuffer{
		entries: make([]OutputEntry, cap),
		cap:     cap,
	}
}

// Write adds an entry to the ring buffer.
func (r *RingBuffer) Write(entry OutputEntry) {
	if r.count == 0 {
		r.minSeq = entry.Seq
	}
	if r.count < r.cap {
		r.entries[r.tail] = entry
		r.tail = (r.tail + 1) % r.cap
		r.count++
	} else {
		// Overwrite oldest
		r.entries[r.head] = entry
		r.head = (r.head + 1) % r.cap
		r.tail = (r.tail + 1) % r.cap
		r.minSeq = r.entries[r.head].Seq
	}
}

// ReadSince returns all entries with sequence number > sinceSeq.
// Returns the entries in order.
func (r *RingBuffer) ReadSince(sinceSeq uint64) []OutputEntry {
	if r.count == 0 {
		return nil
	}

	var result []OutputEntry
	for i := 0; i < r.count; i++ {
		idx := (r.head + i) % r.cap
		if r.entries[idx].Seq > sinceSeq {
			result = append(result, r.entries[idx])
		}
	}
	return result
}

// LatestSeq returns the sequence number of the most recent entry.
// Returns 0 if the buffer is empty.
func (r *RingBuffer) LatestSeq() uint64 {
	if r.count == 0 {
		return 0
	}
	idx := (r.tail - 1 + r.cap) % r.cap
	return r.entries[idx].Seq
}