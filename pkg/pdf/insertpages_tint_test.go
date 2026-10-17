package pdf_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// TestInsertPagesKeepsSeparationsConsistent pins that copying the page of the
// litho fnlimits_1b fixture twice, as the corpus gate's mutate-pages cell
// does, adds no rule at PDF/A-2b. The copies hold the page's Separations
// again, over copies of their tint transforms, and ISO 19005-2, 6.2.4.4 t2
// asks only that each colorant name keep one alternate space and tint
// transform: veraPDF 1.30.2 compares two tint transforms by content
// (COSStream.equals), so a copy is the same tint transform. Measured with
// veraPDF 1.30.2 at PDF/A-2b on this output: no 6.2.4.4 t2.
//
// The copies come from a second Reader, as a caller merging another document
// supplies them, so the Writer imports them as objects of their own rather than
// guessing that two Readers share one: ISO 19005-2 asks for one definition per
// colorant, not one object.
func TestInsertPagesKeepsSeparationsConsistent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data, err := os.ReadFile("../../test/integration/corpus/litho/pdfs/fnlimits_1b.pdf")
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}

	rules := func(t *testing.T, data []byte) []string {
		t.Helper()
		r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer r.Close()
		viols, err := r.ValidatePDFAReport(ctx, models.PDFA2b)
		if err != nil {
			t.Fatalf("ValidatePDFAReport: %v", err)
		}
		var out []string
		for _, v := range viols {
			id := fmt.Sprintf("%s-t%d", v.Clause, v.TestNumber)
			if !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
		slices.Sort(out)
		return out
	}

	src, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer src.Close()
	copySrc, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer copySrc.Close()
	w := pdf.NewWriter(src)
	if err := w.InsertPages(1, copySrc, 1, 1); err != nil {
		t.Fatalf("InsertPages: %v", err)
	}
	var buf bytes.Buffer
	if err := w.WriteTo(ctx, &buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	before, after := rules(t, data), rules(t, buf.Bytes())
	for _, id := range after {
		if !slices.Contains(before, id) {
			t.Errorf("copying the page added %s (before %v, after %v)", id, before, after)
		}
	}
}
