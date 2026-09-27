package pdf

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// malformedBoxPDF builds a four-page document whose page boundaries are no
// rectangles, or enclose no area, in each way a rewrite repairs. The root
// states a /CropBox, [0 0 500 700].
//
//   - Page A states /MediaBox [0 0 600 800 7], its first four entries a
//     rectangle, /CropBox [0 0 300], /TrimBox [0 /Foo 200 300], /BleedBox of
//     five entries and /ArtBox [0 0 400 null].
//   - Page B states /MediaBox [0 0 612], and inherits the root's crop box.
//   - Pages C and D sit under a node stating /MediaBox [0 0 400 500 /X] and
//     /CropBox [0 0 0 0]. C states nothing, D /MediaBox [0 0 0 0].
func malformedBoxPDF() []byte {
	const content = "BT /F1 12 Tf 50 50 Td (Boxes) Tj ET"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R] /Count 4 /CropBox [0 0 500 700] /Resources << /Font << /F1 9 0 R >> >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 600 800 7] /CropBox [0 0 300] /TrimBox [0 /Foo 200 300] " +
			"/BleedBox [0 0 300 400 9] /ArtBox [0 0 400 null] /Contents 8 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612] /Contents 8 0 R >>",
		"<< /Type /Pages /Parent 2 0 R /Kids [6 0 R 7 0 R] /Count 2 /MediaBox [0 0 400 500 /X] /CropBox [0 0 0 0] >>",
		"<< /Type /Page /Parent 5 0 R /Contents 8 0 R >>",
		"<< /Type /Page /Parent 5 0 R /MediaBox [0 0 0 0] /Contents 8 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	})
}

// boxValues returns the numbers of a rectangle, nil for nil.
func boxValues(box ast.PdfArray) []float64 {
	if box == nil {
		return nil
	}
	values := make([]float64, 0, len(box))
	for _, v := range box {
		values = append(values, document.Float(v))
	}
	return values
}

// TestRewrites_WriteThePageBoxesViewersRead pins that every rewrite writes each
// page boundary as a viewer reads it, so no rewrite changes how a page looks.
// ISO 32000-1, 7.9.5 and Table 30 make each boundary a rectangle, four
// numbers, and pdf.js and poppler read any other array, or four numbers
// enclosing no area, as no box: a media box as US Letter, which the rewrite
// writes, and a crop, bleed, trim or art box as its Table 30 default, which
// the rewrite leaves to the reader by dropping the box. A crop box it drops
// would let the root's show through, so the page states its media box
// instead. Each output page measures what [Reader.Page] measured on the
// source.
//
// Measured on the pinned pdf.js, whose page view is the region drawn: the
// source shows pages A, C and D at 612 by 792 and page B at 500 by 700, and so
// does every output. poppler 26.09.0 shows page A at the root's crop box
// instead, since it passes over a box it cannot read and takes the node's (see
// [document.UsableBox]).
func TestRewrites_WriteThePageBoxesViewersRead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := malformedBoxPDF()

	rewrite := func(t *testing.T, edit func(w *Writer) error) []byte {
		t.Helper()
		source := openBytes(t, src)
		defer source.Close()
		w := NewWriter(source)
		defer w.Close()
		if err := edit(w); err != nil {
			t.Fatalf("queueing the edit: %v", err)
		}
		var out bytes.Buffer
		if err := w.WriteTo(ctx, &out, WithFullRewrite()); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		return out.Bytes()
	}
	paths := []struct {
		name  string
		write func(t *testing.T) []byte
		first int
	}{
		{"full rewrite", func(t *testing.T) []byte {
			return rewrite(t, func(*Writer) error { return nil })
		}, 0},
		{"remediation", func(t *testing.T) []byte {
			return rewrite(t, func(w *Writer) error {
				_, err := w.RemediatePDFA(ctx, models.PDFA1b)
				return err
			})
		}, 0},
		// ISO 19005-2, 6.1.13 t11 fails the boxes enclosing no area, so the
		// remediation repairs them itself before the output is written.
		{"remediation of the page boxes", func(t *testing.T) []byte {
			source := openBytes(t, src)
			defer source.Close()
			w := NewWriter(source)
			defer w.Close()
			if _, err := w.RemediatePDFA(ctx, models.PDFA2b); err != nil {
				t.Fatalf("RemediatePDFA: %v", err)
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			return out.Bytes()
		}, 0},
		{"split", func(t *testing.T) []byte {
			source := openBytes(t, src)
			defer source.Close()
			var out bytes.Buffer
			if err := Split(ctx, source, []int{1, 2, 3, 4}, &out); err != nil {
				t.Fatalf("Split: %v", err)
			}
			return out.Bytes()
		}, 0},
		{"merge", func(t *testing.T) []byte {
			first, source := openBytes(t, labelledPDF(1, "")), openBytes(t, src)
			defer first.Close()
			defer source.Close()
			var out bytes.Buffer
			if err := Merge(ctx, []*Reader{first, source}, &out); err != nil {
				t.Fatalf("Merge: %v", err)
			}
			return out.Bytes()
		}, 1},
		{"insert", func(t *testing.T) []byte {
			source := openBytes(t, src)
			defer source.Close()
			return writeLabelled(t, labelledPDF(1, ""), func(w *Writer) error {
				return w.InsertPages(1, source)
			})
		}, 1},
	}
	letter := []float64{0, 0, 612, 792}
	want := []struct {
		media, crop, trim, bleed []float64
	}{
		{letter, letter, nil, nil},
		{letter, []float64{0, 0, 500, 700}, nil, nil},
		{letter, letter, nil, nil},
		{letter, letter, nil, nil},
	}
	sizes := pageSizes(t, src)
	for _, path := range paths {
		t.Run(path.name, func(t *testing.T) {
			t.Parallel()
			data := path.write(t)
			o := readOutputTree(t, data)
			if got := len(o.pages); got != path.first+len(want) {
				t.Fatalf("the output has %d pages, want %d", got, path.first+len(want))
			}
			if got := pageSizes(t, data)[path.first:]; !slices.Equal(got, sizes) {
				t.Errorf("the output pages measure %v, want the %v the source pages measure", got, sizes)
			}
			for i, w := range want {
				page := o.pages[path.first+i]
				for _, box := range []struct {
					name  string
					value ast.PdfArray
				}{
					{"MediaBox", page.MediaBox}, {"CropBox", page.DeclaredCropBox}, {"BleedBox", page.DeclaredBleedBox},
					{"TrimBox", page.DeclaredTrimBox}, {"ArtBox", page.DeclaredArtBox},
				} {
					if box.value != nil && !isRectangle(box.value) {
						t.Errorf("page %d states /%s %v, want a rectangle", i+1, box.name, box.value)
					}
				}
				if got := boxValues(page.MediaBox); !slices.Equal(got, w.media) {
					t.Errorf("page %d /MediaBox reads %v, want %v", i+1, got, w.media)
				}
				if got := boxValues(page.CropBox); !slices.Equal(got, w.crop) {
					t.Errorf("page %d crop box reads %v, want %v", i+1, got, w.crop)
				}
				if got := boxValues(page.DeclaredTrimBox); !slices.Equal(got, w.trim) {
					t.Errorf("page %d states /TrimBox %v, want %v", i+1, got, w.trim)
				}
				if got := boxValues(page.DeclaredBleedBox); !slices.Equal(got, w.bleed) {
					t.Errorf("page %d states /BleedBox %v, want %v", i+1, got, w.bleed)
				}
				if page.DeclaredArtBox != nil {
					t.Errorf("page %d states /ArtBox %v, want none", i+1, page.DeclaredArtBox)
				}
			}
		})
	}
}

// pageSizes returns the size [Reader.Page] reports for each page of data.
func pageSizes(t *testing.T, data []byte) [][2]float64 {
	t.Helper()
	r := openBytes(t, data)
	defer r.Close()
	n, err := r.PageCount(context.Background())
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	sizes := make([][2]float64, 0, n)
	for i := 1; i <= n; i++ {
		p, err := r.Page(context.Background(), i)
		if err != nil {
			t.Fatalf("Page(%d): %v", i, err)
		}
		sizes = append(sizes, [2]float64{p.Width, p.Height})
	}
	return sizes
}
