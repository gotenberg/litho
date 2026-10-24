package content

import (
	"context"
	"testing"
)

// TestInlineImageSamples pins the spans [InlineImageSamples] returns: the data
// between the white space after ID and the white space before EI, ending
// where the earlier of veraPDF and PDF.js ends it.
func TestInlineImageSamples(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, stream string
		want         []string
	}{
		{"none", "BT (ID) Tj ET", nil},
		{"one", "q BI /W 2 /H 1 /CS /G /BPC 8 ID \x00\xff EI Q", []string{"\x00\xff"}},
		{"two", "BI /W 1 /H 1 /CS /G /BPC 8 ID A EI BT (x) Tj ET BI /W 1 /H 1 /CS /G /BPC 8 ID\nB\nEI", []string{"A", "B"}},
		// PDF.js ends the data at the first EI, veraPDF at the second.
		{"readers disagree", "BI /W 40 /H 1 /CS /G /BPC 8 ID \x00\x01 EI SECRET EI Q", []string{"\x00\x01"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := []byte(tc.stream)
			spans, err := InlineImageSamples(context.Background(), data)
			if err != nil {
				t.Fatal(err)
			}
			if len(spans) != len(tc.want) {
				t.Fatalf("spans %v, want %d", spans, len(tc.want))
			}
			for i, span := range spans {
				if got := string(data[span.Start:span.End]); got != tc.want[i] {
					t.Errorf("span %d holds %q, want %q", i, got, tc.want[i])
				}
			}
		})
	}
}

// FuzzInlineImageSamples checks that the spans never panic, lie within the
// stream, and come in order without overlapping.
func FuzzInlineImageSamples(f *testing.F) {
	f.Add([]byte("q BI /W 2 /H 1 /CS /G /BPC 8 ID \x00\xff EI Q"))
	f.Add([]byte("q BI /W 2 /H 1 /CS /G /BPC 8 ID \x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		spans, err := InlineImageSamples(context.Background(), data)
		if err != nil {
			return
		}
		from := 0
		for _, span := range spans {
			if span.Start < from || span.End < span.Start || span.End > len(data) {
				t.Fatalf("span %v out of order or bounds (from %d, len %d)", span, from, len(data))
			}
			from = span.End
		}
	})
}
