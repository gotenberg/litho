package icc

import "testing"

// TestCMYKPrinterProfileReadsAsDeviceCMYK pins that the stock CMYK printer
// profile is a version 2 output profile litho's engine builds a transform
// from, and that it reads every CMYK colour as the built-in DeviceCMYK
// reading does.
func TestCMYKPrinterProfileReadsAsDeviceCMYK(t *testing.T) {
	t.Parallel()

	p, err := Parse(CMYKPrinterProfile())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.Version != 2 || p.Space != "CMYK" || p.PCS != "Lab " {
		t.Fatalf("version %d, space %q, PCS %q", p.Version, p.Space, p.PCS)
	}
	if got := string(CMYKPrinterProfile()[12:16]); got != "prtr" {
		t.Errorf("device class %q, want prtr, which ISO 19005-1, 6.2.2 admits in an output intent", got)
	}
	for _, intent := range []Intent{Perceptual, RelativeColorimetric} {
		tr, err := p.NewTransform(intent)
		if err != nil {
			t.Fatalf("NewTransform(%d): %v", intent, err)
		}
		builtin, err := BuiltinCMYK(intent)
		if err != nil {
			t.Fatalf("BuiltinCMYK(%d): %v", intent, err)
		}
		for i := range 6 * 6 * 6 * 6 {
			in := []byte{byte(i / 216 * 51), byte(i / 36 % 6 * 51), byte(i / 6 % 6 * 51), byte(i % 6 * 51)}
			r, g, b := tr.Convert(in)
			wr, wg, wb := builtin.Convert(in)
			if r != wr || g != wg || b != wb {
				t.Fatalf("intent %d: CMYK %v reads %d,%d,%d, built-in %d,%d,%d", intent, in, r, g, b, wr, wg, wb)
			}
		}
	}
}
