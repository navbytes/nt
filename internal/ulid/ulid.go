// Package ulid generates ULIDs (Universally Unique Lexicographically Sortable
// IDs) with zero external dependencies. A ULID is a 128-bit value: a 48-bit
// millisecond timestamp followed by 80 bits of randomness, encoded as 26
// Crockford base32 characters. Lexical order matches creation order, which is
// exactly the "sortable by creation time" property nt relies on (SPEC §4).
package ulid

import (
	"crypto/rand"
	"sync"
	"time"
)

// Crockford base32 alphabet (no I, L, O, U).
const encoding = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var (
	mu       sync.Mutex
	lastMS   uint64
	lastRand [10]byte
)

// New returns a fresh ULID string. It is monotonic within a process: two ULIDs
// generated in the same millisecond still sort in generation order, so a burst
// of `nt add` calls (e.g. from an AI session) keep a stable order.
func New() string {
	mu.Lock()
	defer mu.Unlock()

	ms := uint64(time.Now().UnixMilli())
	var entropy [10]byte
	if ms == lastMS {
		// Same millisecond: increment the previous randomness so order holds.
		entropy = lastRand
		incr(&entropy)
	} else {
		_, _ = rand.Read(entropy[:])
		lastMS = ms
	}
	lastRand = entropy

	var id [16]byte
	id[0] = byte(ms >> 40)
	id[1] = byte(ms >> 32)
	id[2] = byte(ms >> 24)
	id[3] = byte(ms >> 16)
	id[4] = byte(ms >> 8)
	id[5] = byte(ms)
	copy(id[6:], entropy[:])
	return encode(id)
}

// incr adds 1 to the 80-bit big-endian entropy value.
func incr(e *[10]byte) {
	for i := 9; i >= 0; i-- {
		e[i]++
		if e[i] != 0 {
			break
		}
	}
}

// decodeTbl maps a Crockford base32 byte to its 5-bit value (-1 if invalid).
var decodeTbl = func() [256]int8 {
	var t [256]int8
	for i := range t {
		t[i] = -1
	}
	for i := 0; i < len(encoding); i++ {
		t[encoding[i]] = int8(i)
	}
	return t
}()

// Time extracts the creation time from a ULID's 48-bit millisecond timestamp
// prefix (the first 10 characters). ok is false if id is too short or holds an
// invalid character. This is the inverse of the time bits New encodes, letting
// callers recover "when was this created" without storing a separate field.
func Time(id string) (t time.Time, ok bool) {
	if len(id) < 10 {
		return time.Time{}, false
	}
	var b [10]uint64
	for i := 0; i < 10; i++ {
		v := decodeTbl[id[i]]
		if v < 0 {
			return time.Time{}, false
		}
		b[i] = uint64(v)
	}
	// The first 10 chars hold the 48-bit ms timestamp, left-padded with two zero
	// bits (the first char spans only 3 bits), so the 50-bit concatenation is the
	// timestamp directly.
	ms := b[0]<<45 | b[1]<<40 | b[2]<<35 | b[3]<<30 | b[4]<<25 |
		b[5]<<20 | b[6]<<15 | b[7]<<10 | b[8]<<5 | b[9]
	return time.UnixMilli(int64(ms)), true
}

// encode renders the 16-byte id as 26 Crockford base32 characters.
func encode(id [16]byte) string {
	dst := make([]byte, 26)
	dst[0] = encoding[(id[0]&224)>>5]
	dst[1] = encoding[id[0]&31]
	dst[2] = encoding[(id[1]&248)>>3]
	dst[3] = encoding[((id[1]&7)<<2)|((id[2]&192)>>6)]
	dst[4] = encoding[(id[2]&62)>>1]
	dst[5] = encoding[((id[2]&1)<<4)|((id[3]&240)>>4)]
	dst[6] = encoding[((id[3]&15)<<1)|((id[4]&128)>>7)]
	dst[7] = encoding[(id[4]&124)>>2]
	dst[8] = encoding[((id[4]&3)<<3)|((id[5]&224)>>5)]
	dst[9] = encoding[id[5]&31]
	dst[10] = encoding[(id[6]&248)>>3]
	dst[11] = encoding[((id[6]&7)<<2)|((id[7]&192)>>6)]
	dst[12] = encoding[(id[7]&62)>>1]
	dst[13] = encoding[((id[7]&1)<<4)|((id[8]&240)>>4)]
	dst[14] = encoding[((id[8]&15)<<1)|((id[9]&128)>>7)]
	dst[15] = encoding[(id[9]&124)>>2]
	dst[16] = encoding[((id[9]&3)<<3)|((id[10]&224)>>5)]
	dst[17] = encoding[id[10]&31]
	dst[18] = encoding[(id[11]&248)>>3]
	dst[19] = encoding[((id[11]&7)<<2)|((id[12]&192)>>6)]
	dst[20] = encoding[(id[12]&62)>>1]
	dst[21] = encoding[((id[12]&1)<<4)|((id[13]&240)>>4)]
	dst[22] = encoding[((id[13]&15)<<1)|((id[14]&128)>>7)]
	dst[23] = encoding[(id[14]&124)>>2]
	dst[24] = encoding[((id[14]&3)<<3)|((id[15]&224)>>5)]
	dst[25] = encoding[id[15]&31]
	return string(dst)
}
