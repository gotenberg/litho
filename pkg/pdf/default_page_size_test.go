package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// a4 is the A4 page litho's fixtures state, in points.
var a4 = [2]float64{595.28, 841.89}

// sizedPagesPDF builds one page per entry of boxes, each stating the /MediaBox
// written there, none when it is empty, and drawing its page number in the
// lower-left corner.
func sizedPagesPDF(boxes ...string) []byte {
	kids := make([]string, len(boxes))
	objs := []string{"<< /Type /Catalog /Pages 2 0 R >>", "", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>"}
	for i, box := range boxes {
		pageNum := len(objs) + 1
		kids[i] = fmt.Sprintf("%d 0 R", pageNum)
		entry := ""
		if box != "" {
			entry = " /MediaBox " + box
		}
		content := fmt.Sprintf("BT /F1 12 Tf 20 20 Td (Page %d) Tj ET", i+1)
		objs = append(objs,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R%s /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", entry, pageNum+1),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	}
	objs[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(boxes))
	return buildPDF(objs)
}

// a4WithMissingBoxPDF is three A4 pages, a US Letter one, and a page, the
// third, stating no /MediaBox. A viewer draws it on US Letter.
func a4WithMissingBoxPDF() []byte {
	a4Box := fmt.Sprintf("[0 0 %g %g]", a4[0], a4[1])
	return sizedPagesPDF(a4Box, "[0 0 612 792]", "", a4Box, a4Box)
}

// sizeOf returns the size [Reader.Page] reports for page pageNum of data.
func sizeOf(t *testing.T, data []byte, pageNum int) [2]float64 {
	t.Helper()
	sizes := pageSizes(t, data)
	if pageNum > len(sizes) {
		t.Fatalf("the output has %d pages, want at least %d", len(sizes), pageNum)
	}
	return sizes[pageNum-1]
}

// TestWrites_GiveAMissingMediaBoxTheDocumentSize pins that every write that
// gives a page with no media box one gives it the size most pages of the
// document have, and that [Reader.Page] keeps reading the source as a viewer
// shows it, US Letter. ISO 32000-1, Table 30 makes /MediaBox required and
// gives it no default.
func TestWrites_GiveAMissingMediaBoxTheDocumentSize(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := a4WithMissingBoxPDF()
	if got := sizeOf(t, src, 3); got != [2]float64{612, 792} {
		t.Fatalf("the source's page 3 reads %v, want the US Letter a viewer shows", got)
	}

	writeWith := func(t *testing.T, edit func(w *Writer) error, opts ...WriteOption) []byte {
		t.Helper()
		r := openBytes(t, src)
		defer r.Close()
		w := NewWriter(r)
		defer w.Close()
		if err := edit(w); err != nil {
			t.Fatalf("queueing the edit: %v", err)
		}
		var out bytes.Buffer
		if err := w.WriteTo(ctx, &out, opts...); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		return out.Bytes()
	}
	for _, tc := range []struct {
		name  string
		write func(t *testing.T) []byte
		page  int
	}{
		{"full rewrite", func(t *testing.T) []byte {
			return writeWith(t, func(*Writer) error { return nil }, WithFullRewrite())
		}, 3},
		{"incremental update writing the page", func(t *testing.T) []byte {
			return writeWith(t, func(w *Writer) error { return w.RotatePages(90, 3) })
		}, 3},
		{"delete", func(t *testing.T) []byte {
			return writeWith(t, func(w *Writer) error { return w.DeletePages(1) })
		}, 2},
		{"split of the page alone", func(t *testing.T) []byte {
			r := openBytes(t, src)
			defer r.Close()
			var out bytes.Buffer
			if err := Split(ctx, r, []int{3}, &out); err != nil {
				t.Fatalf("Split: %v", err)
			}
			return out.Bytes()
		}, 1},
		{"merge", func(t *testing.T) []byte {
			first, second := openBytes(t, letterPDF()), openBytes(t, src)
			defer first.Close()
			defer second.Close()
			var out bytes.Buffer
			if err := Merge(ctx, []*Reader{first, second}, &out); err != nil {
				t.Fatalf("Merge: %v", err)
			}
			return out.Bytes()
		}, 4},
		{"insert into an A4 document", func(t *testing.T) []byte {
			a4Box := fmt.Sprintf("[0 0 %g %g]", a4[0], a4[1])
			return writeLabelled(t, sizedPagesPDF(a4Box, a4Box), func(w *Writer) error {
				return w.InsertPages(1, openBytes(t, sizedPagesPDF("")))
			})
		}, 2},
		{"remediation", func(t *testing.T) []byte {
			return writeWith(t, func(w *Writer) error {
				_, err := w.RemediatePDFA(ctx, models.PDFA2b)
				return err
			})
		}, 3},
		{"pipeline", func(t *testing.T) []byte {
			r := openBytes(t, src)
			defer r.Close()
			var out bytes.Buffer
			p := NewPipeline().Open(r).Split([]int{3, 4}).Apply(func(_ context.Context, w *Writer) error { return w.RotatePages(90, 1) })
			if _, err := p.WriteTo(ctx, &out); err != nil {
				t.Fatalf("Pipeline.WriteTo: %v", err)
			}
			return out.Bytes()
		}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := tc.write(t)
			got := sizeOf(t, out, tc.page)
			if tc.name == "incremental update writing the page" || tc.name == "pipeline" {
				// Rotated a quarter turn, the page reports its sides swapped.
				got = [2]float64{got[1], got[0]}
			}
			if !almostEqual(got[0], a4[0], 0.01) || !almostEqual(got[1], a4[1], 0.01) {
				t.Errorf("page %d measures %v, want the A4 %v most pages of the document have", tc.page, got, a4)
			}
			r := openBytes(t, out)
			defer r.Close()
			if text := mustText(t, r, tc.page); tc.name != "insert into an A4 document" && !strings.Contains(text, "Page 3") {
				t.Errorf("page %d reads %q, want the text of the source's page 3", tc.page, text)
			}
		})
	}
}

// TestWithDefaultPageSize_SizesADocumentWithNoMediaBox pins that
// [WithDefaultPageSize] names the size of a page with no media box when no
// page of the document has one, on each write path, and that US Letter is the
// default.
func TestWithDefaultPageSize_SizesADocumentWithNoMediaBox(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := sizedPagesPDF("", "[0 0 0 0]")
	opt := WithDefaultPageSize(a4[0], a4[1])

	rewrite := func(t *testing.T, newWriterOpts []WriteOption, remediate bool, opts ...WriteOption) []byte {
		t.Helper()
		r := openBytes(t, src)
		defer r.Close()
		w := NewWriter(r, newWriterOpts...)
		defer w.Close()
		if remediate {
			if _, err := w.RemediatePDFA(ctx, models.PDFA2b); err != nil {
				t.Fatalf("RemediatePDFA: %v", err)
			}
		}
		var out bytes.Buffer
		if err := w.WriteTo(ctx, &out, opts...); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		return out.Bytes()
	}
	for _, tc := range []struct {
		name  string
		write func(t *testing.T) []byte
		want  [2]float64
	}{
		{"default", func(t *testing.T) []byte {
			return rewrite(t, nil, false, WithFullRewrite())
		}, [2]float64{612, 792}},
		{"WriteTo", func(t *testing.T) []byte {
			return rewrite(t, nil, false, WithFullRewrite(), opt)
		}, a4},
		{"NewWriter", func(t *testing.T) []byte {
			return rewrite(t, []WriteOption{opt}, false, WithFullRewrite())
		}, a4},
		{"WriteTo over NewWriter", func(t *testing.T) []byte {
			return rewrite(t, []WriteOption{opt}, false, WithFullRewrite(), WithDefaultPageSize(612, 1008))
		}, [2]float64{612, 1008}},
		{"remediation", func(t *testing.T) []byte {
			return rewrite(t, []WriteOption{opt}, true)
		}, a4},
		{"split", func(t *testing.T) []byte {
			r := openBytes(t, src)
			defer r.Close()
			var out bytes.Buffer
			if err := Split(ctx, r, []int{1, 2}, &out, opt); err != nil {
				t.Fatalf("Split: %v", err)
			}
			return out.Bytes()
		}, a4},
		{"merge", func(t *testing.T) []byte {
			r := openBytes(t, src)
			defer r.Close()
			var out bytes.Buffer
			if err := Merge(ctx, []*Reader{r}, &out, opt); err != nil {
				t.Fatalf("Merge: %v", err)
			}
			return out.Bytes()
		}, a4},
		{"pipeline, intermediate split", func(t *testing.T) []byte {
			r := openBytes(t, src)
			defer r.Close()
			var out bytes.Buffer
			p := NewPipeline().Open(r).Split([]int{1, 2}).Apply(func(_ context.Context, w *Writer) error {
				w.SetMetadata(Metadata{Title: "t"})
				return nil
			})
			if _, err := p.WriteTo(ctx, &out, opt); err != nil {
				t.Fatalf("Pipeline.WriteTo: %v", err)
			}
			return out.Bytes()
		}, a4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for i, got := range pageSizes(t, tc.write(t)) {
				if !almostEqual(got[0], tc.want[0], 0.01) || !almostEqual(got[1], tc.want[1], 0.01) {
					t.Errorf("page %d measures %v, want %v", i+1, got, tc.want)
				}
			}
		})
	}
}

// TestWithDefaultPageSize_RefusesASizeNoPageTakes pins that a default page
// size outside ISO 32000-1, Annex C.2, Table C.1 fails every write with
// [ErrInvalidPageSize], and a later valid one replaces it.
func TestWithDefaultPageSize_RefusesASizeNoPageTakes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := sizedPagesPDF("")
	for _, size := range [][2]float64{{0, 792}, {612, 2}, {14401, 792}, {-612, 792}} {
		bad := WithDefaultPageSize(size[0], size[1])
		r := openBytes(t, src)
		var out bytes.Buffer
		if err := Split(ctx, r, []int{1}, &out, bad); !errors.Is(err, ErrInvalidPageSize) {
			t.Errorf("Split with %v returned %v, want ErrInvalidPageSize", size, err)
		}
		if err := Merge(ctx, []*Reader{r}, &out, bad); !errors.Is(err, ErrInvalidPageSize) {
			t.Errorf("Merge with %v returned %v, want ErrInvalidPageSize", size, err)
		}
		if _, err := NewPipeline().Open(r).WriteTo(ctx, &out, bad); !errors.Is(err, ErrInvalidPageSize) {
			t.Errorf("Pipeline.WriteTo with %v returned %v, want ErrInvalidPageSize", size, err)
		}
		w := NewWriter(r)
		if err := w.WriteTo(ctx, &out, bad); !errors.Is(err, ErrInvalidPageSize) {
			t.Errorf("WriteTo with %v returned %v, want ErrInvalidPageSize", size, err)
		}
		if err := w.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
		w = NewWriter(r, bad)
		if _, err := w.RemediatePDFA(ctx, models.PDFA2b); !errors.Is(err, ErrInvalidPageSize) {
			t.Errorf("RemediatePDFA after NewWriter with %v returned %v, want ErrInvalidPageSize", size, err)
		}
		if err := w.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
		out.Reset()
		if err := Split(ctx, r, []int{1}, &out, bad, WithDefaultPageSize(612, 792)); err != nil {
			t.Errorf("Split with a valid size after %v: %v", size, err)
		}
		if err := r.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}
}

// TestWrites_ClipACropBoxAgainstTheDefaultMediaBox pins that a page with no
// media box keeps the crop box it states, which a viewer clips against the
// media box the write gives the page rather than against US Letter.
func TestWrites_ClipACropBoxAgainstTheDefaultMediaBox(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	a4Box := fmt.Sprintf("[0 0 %g %g]", a4[0], a4[1])
	src := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox " + a4Box + " >>",
		"<< /Type /Page /Parent 2 0 R /CropBox [0 0 590 830] >>",
	})
	if got := sizeOf(t, src, 2); got != [2]float64{590, 792} {
		t.Fatalf("the source's page 2 reads %v, want its crop box clipped to US Letter", got)
	}
	r := openBytes(t, src)
	defer r.Close()
	var split bytes.Buffer
	if err := Split(ctx, r, []int{2}, &split); err != nil {
		t.Fatalf("Split: %v", err)
	}
	rewritten := writeLabelled(t, src, func(w *Writer) error { return w.RotatePages(180, 2) })
	for name, out := range map[string][]byte{"split": split.Bytes(), "rewrite": rewritten} {
		page := 1
		if name == "rewrite" {
			page = 2
		}
		if got := sizeOf(t, out, page); got != [2]float64{590, 830} {
			t.Errorf("%s: the page measures %v, want its crop box [0 0 590 830] inside the A4 media box", name, got)
		}
	}
	if slices.Equal(split.Bytes(), rewritten) {
		t.Fatal("the two writes produced the same bytes, so the test compares nothing")
	}
}
