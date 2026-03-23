package filter

import (
	"bytes"
	"fmt"
)

// decodeRunLength implements the RunLengthDecode filter (PDF spec 7.4.5).
// It uses the PackBits compression scheme:
//   - A length byte n in 0–127 means copy the next (n+1) literal bytes.
//   - A length byte n in 129–255 means repeat the next byte (257-n) times.
//   - Byte 128 (0x80) is the EOD marker and terminates the stream.
func decodeRunLength(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	i := 0

	for i < len(data) {
		n := data[i]
		i++

		switch {
		case n < 128:
			// Copy next (n+1) literal bytes.
			count := int(n) + 1
			if i+count > len(data) {
				return nil, fmt.Errorf("runlength decode: need %d literal bytes at offset %d but only %d remain", count, i, len(data)-i)
			}
			buf.Write(data[i : i+count])
			i += count

		case n > 128:
			// Repeat next single byte (257-n) times.
			if i >= len(data) {
				return nil, fmt.Errorf("runlength decode: missing repeat byte at offset %d", i)
			}
			count := 257 - int(n)
			b := data[i]
			i++
			for range count {
				buf.WriteByte(b)
			}

		default:
			// n == 128: EOD marker.
			return buf.Bytes(), nil
		}
	}

	return buf.Bytes(), nil
}
