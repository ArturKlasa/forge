// Package proc manages subprocess lifecycle for forge agent runs.
package proc

import (
	"sync"
)

const defaultRingBufferSize = 64 * 1024 // 64 KiB

// RingBuffer is a thread-safe fixed-size ring buffer that implements io.Writer.
// It retains only the last N bytes written. Reads via Bytes() return the current
// contents in order from oldest to newest byte.
type RingBuffer struct {
	mu   sync.Mutex
	buf  []byte
	pos  int  // next write position (wraps around)
	full bool // true once the buffer has been written past its capacity
}

// NewRingBuffer returns a RingBuffer with the given capacity.
// If size <= 0, defaultRingBufferSize is used.
func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = defaultRingBufferSize
	}
	return &RingBuffer{buf: make([]byte, size)}
}

// Write implements io.Writer. It writes p into the ring buffer, discarding
// the oldest bytes when capacity is exceeded. Always returns len(p), nil.
func (r *RingBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if n == 0 {
		return 0, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	cap := len(r.buf)

	// If p is larger than the ring buffer, we only care about the last cap bytes.
	if n >= cap {
		copy(r.buf, p[n-cap:])
		r.pos = 0
		r.full = true
		return n, nil
	}

	// How many bytes fit from pos to end of buffer?
	tail := cap - r.pos
	if n <= tail {
		copy(r.buf[r.pos:], p)
		r.pos += n
		if r.pos == cap {
			r.pos = 0
			r.full = true
		}
	} else {
		// Split across the wrap boundary.
		copy(r.buf[r.pos:], p[:tail])
		copy(r.buf, p[tail:])
		r.pos = n - tail
		r.full = true
	}

	return n, nil
}

// Bytes returns a copy of the current ring buffer contents in order
// (oldest byte first). The returned slice is independent of the buffer.
func (r *RingBuffer) Bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.full {
		// Buffer has not wrapped; live data is buf[0:pos].
		out := make([]byte, r.pos)
		copy(out, r.buf[:r.pos])
		return out
	}

	// Buffer has wrapped; oldest data starts at pos.
	out := make([]byte, len(r.buf))
	copy(out, r.buf[r.pos:])
	copy(out[len(r.buf)-r.pos:], r.buf[:r.pos])
	return out
}

// Len returns the number of bytes currently held in the buffer.
func (r *RingBuffer) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.full {
		return len(r.buf)
	}
	return r.pos
}
