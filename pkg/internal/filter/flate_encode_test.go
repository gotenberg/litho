package filter

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestEncodeFlateSamples_CountsTheParmsCost pins that the predicted
// encoding wins only when it saves more than parmsCost, whichever encoding
// is tried first, around the cost where the choice turns, and that what it
// returns decodes to the samples through the /DecodeParms it returns.
func TestEncodeFlateSamples_CountsTheParmsCost(t *testing.T) {
	const width, height, comps = 97, 61, 3
	samples := make([]byte, width*height*comps)
	for i := range samples {
		px := i / comps
		samples[i] = byte(px%width*(i%comps+1) + px/width) //nolint:gosec // Wrapping is the point.
	}
	plain, _, err := CompressFlate(samples, -1)
	if err != nil {
		t.Fatal(err)
	}
	predicted, err := EncodePNGPredictor(samples, comps, 8, width)
	if err != nil {
		t.Fatal(err)
	}
	packed, _, err := CompressFlate(predicted, -1)
	if err != nil {
		t.Fatal(err)
	}
	saving := len(plain) - len(packed)
	if saving <= 1 {
		t.Fatalf("prediction saves %d bytes: the test checks less than it says", saving)
	}
	for _, cost := range []int{0, saving - 1, saving, saving + 1} {
		wantPredicted := len(packed)+cost < len(plain)
		for _, predictFirst := range []bool{false, true} {
			got, parms, fits, err := EncodeFlateSamples(samples, width, comps, 8, -1, cost, predictFirst)
			if err != nil || !fits {
				t.Fatalf("cost %d: fits %v, %v", cost, fits, err)
			}
			if (parms != nil) != wantPredicted {
				t.Fatalf("cost %d, predict first %v: /DecodeParms %v, want predicted %v", cost, predictFirst, parms, wantPredicted)
			}
			dict := ast.PdfDict{"Filter": ast.PdfName("FlateDecode")}
			if parms != nil {
				dict["DecodeParms"] = parms
			}
			r, err := NewStreamReader(context.Background(), ast.NewStream(dict, got))
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := io.ReadAll(r)
			if closeErr := r.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(decoded, samples) {
				t.Fatalf("cost %d, predict first %v: the encoding does not decode to the samples", cost, predictFirst)
			}
		}
	}
}
