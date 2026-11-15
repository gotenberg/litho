package dct

import (
	"context"
	"errors"
	"testing"
)

// TestQuality_ReadsTheQualityEncodeWrote: the luminance table of a JPEG
// [Encode] wrote at quality q is the table of T.81, Annex K.1 scaled to q, so
// Quality reads q back, for one and for three components.
func TestQuality_ReadsTheQualityEncodeWrote(t *testing.T) {
	for _, comps := range []int{1, 3, 4} {
		for _, q := range []int{10, 25, 50, 75, 90} {
			data, err := Encode(context.Background(), testPhoto(16, 16, comps), 16, 16, comps, EncodeOptions{Quality: q})
			if err != nil {
				t.Fatal(err)
			}
			got, err := Quality(context.Background(), data)
			if err != nil {
				t.Fatal(err)
			}
			if got != q {
				t.Errorf("%d components at quality %d: Quality = %d", comps, q, got)
			}
		}
	}
}

// TestQuality_RefusesWhatHasNoTable: data with no frame, or whose first
// component names a table no DQT segment defined, has no quality to read.
func TestQuality_RefusesWhatHasNoTable(t *testing.T) {
	data, err := Encode(context.Background(), testPhoto(8, 8, 1), 8, 8, 1, EncodeOptions{Quality: 75})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Quality(context.Background(), data[:2]); !errors.Is(err, ErrMalformed) {
		t.Errorf("start-of-image alone: %v, want ErrMalformed", err)
	}
	noDQT := withoutSegment(data, 0xDB)
	if _, err := Quality(context.Background(), noDQT); !errors.Is(err, ErrMalformed) {
		t.Errorf("no DQT segment: %v, want ErrMalformed", err)
	}
}

// withoutSegment returns data with every marker segment of marker m removed,
// the segments before the first scan being whole and unescaped.
func withoutSegment(data []byte, m byte) []byte {
	out := append([]byte(nil), data[:2]...)
	p := 2
	for p+4 <= len(data) && data[p] == 0xFF {
		length := int(data[p+2])<<8 | int(data[p+3])
		if data[p+1] == 0xDA {
			return append(out, data[p:]...)
		}
		if data[p+1] != m {
			out = append(out, data[p:p+2+length]...)
		}
		p += 2 + length
	}
	return out
}
