package zdeflate

import "fmt"

// pyDefBufSize is CPython's DEF_BUF_SIZE, the first output buffer size of
// zlib.compress.
const pyDefBufSize = 16 * 1024

// Compress returns exactly what Python's zlib.compress(data, level) returns
// with zlib 1.2.12: a zlib header, the deflate stream and the Adler-32
// trailer. level is -1 (meaning 6) or 0 to 9; any other value panics.
// data is not modified.
func Compress(data []byte, level int) []byte {
	if level < -1 || level > 9 {
		panic(fmt.Sprintf("zdeflate: invalid compression level %d", level))
	}
	s := newState(level)
	s.in = data
	s.availIn = len(data)

	// arrange_output_buffer: start at DEF_BUF_SIZE, double when full.
	length := pyDefBufSize
	s.out = make([]byte, length)
	s.availOut = length
	for {
		ret := s.deflate(zFinish)
		if ret != zOK && ret != zStreamEnd {
			panic(fmt.Sprintf("zdeflate: deflate returned %d", ret))
		}
		if s.availOut != 0 {
			break
		}
		grown := make([]byte, length*2)
		copy(grown, s.out)
		s.out = grown
		s.availOut = length
		length *= 2
	}
	return s.out[:s.nextOut]
}

// CompressDefault is [Compress] at level 6, zlib.compress's default.
func CompressDefault(data []byte) []byte {
	return Compress(data, 6)
}
