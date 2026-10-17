package pdf

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// pdfa2bRules returns the distinct clause/test pairs data fails at PDF/A-2b.
func pdfa2bRules(ctx context.Context, t *testing.T, data []byte) []string {
	t.Helper()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	violations, err := r.ValidatePDFAReport(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFAReport: %v", err)
	}
	var ids []string
	for _, v := range violations {
		id := fmt.Sprintf("%s/%d", v.Clause, v.TestNumber)
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// TestRedact_ImageColourKeepsPDFA redacts each image of the litho fixture
// imagecolour_2b, and then all of page 1, and validates every output at
// PDF/A-2b against the input. Redaction rewrites a picture it cannot keep in
// its own space in sRGB; stated as DeviceRGB, that wrote the device space the
// input never used, and ISO 19005-2, 6.2.4.3 test 2 forbids it without a
// DefaultRGB space or an RGB output intent. Stated by an ICC profile, it adds
// no rule.
func TestRedact_ImageColourKeepsPDFA(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := readLithoFixture(t, "imagecolour_2b.pdf")
	want := pdfa2bRules(ctx, t, data)

	// The builder draws image i at 80x40 points from (50, 700 - 60i).
	regions := map[string][4]float64{"page": {-1e6, -1e6, 1e6, 1e6}}
	for i, name := range []string{"SepNone", "DevNone", "JLabDef", "JLabEP", "JROMM", "JESRGB"} {
		y := 700 - 60*float64(i)
		regions[name] = [4]float64{50, y, 90, y + 40}
	}
	for name, rect := range regions {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r)
			defer w.Close()
			if _, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: rect}}); err != nil {
				t.Fatalf("Redact: %v", err)
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			got := pdfa2bRules(ctx, t, out.Bytes())
			for _, id := range got {
				if !slices.Contains(want, id) {
					t.Errorf("redaction added %s at PDF/A-2b; input fails %v, output %v", id, want, got)
				}
			}
		})
	}
}
