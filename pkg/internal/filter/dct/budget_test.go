package dct

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"
)

// TestCheck_AdmitsTheImageBudget pins that the default cap admits every frame
// the default image budget admits, in any count of components Decode reads:
// pdf.Limits holds a codestream to the budget its image dictionary is held
// to, and the image decoder takes up to four bytes a pixel of it. The cap
// was 2^27 samples, so a colour JPEG past 44.7 MP was refused although the
// budget admits 100 MP. A frame past four samples a pixel of the budget stays
// refused.
//
// # Corpus evidence
//
//   - pdfjs issue10989.pdf: a 10308x6304 RGB JPEG, 195M samples, whose
//     frame declares 60000 lines. image/jpeg fails on it over that
//     placeholder, PDF.js renders it, and litho renders it as PDF.js does
//     under the larger cap.
func TestCheck_AdmitsTheImageBudget(t *testing.T) {
	t.Parallel()

	frame := func(w, h uint16) []byte {
		data := bytes.Clone(encoded(t, 8, 8, false))
		sof := segment(t, data, 0xC0)
		binary.BigEndian.PutUint16(data[sof+5:], h)
		binary.BigEndian.PutUint16(data[sof+7:], w)
		return data
	}
	ctx := context.Background()
	for name, tc := range map[string]struct {
		data   []byte
		stated int
	}{
		"issue10989 frame":           {frame(10308, 60000), 6304},
		"100 MP in three components": {frame(10000, 10000), 10000},
	} {
		if _, err := Check(ctx, tc.data, tc.stated, DefaultMaxSamples); err != nil {
			t.Errorf("%s: Check = %v, want the frame admitted", name, err)
		}
	}
	if _, err := Check(ctx, frame(20000, 20000), 20000, DefaultMaxSamples); !errors.Is(err, ErrLimit) {
		t.Errorf("400 MP in three components: Check = %v, want ErrLimit", err)
	}
}
