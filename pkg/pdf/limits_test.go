package pdf

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// limitsPDF assembles a one-revision document from objs, numbered from 1, the
// first being the catalog.
func limitsPDF(objs ...string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&b, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return b.Bytes()
}

// streamObj writes a stream object of dict entries and data.
func streamObj(dict string, data []byte) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
}

// openLimited opens data under l.
func openLimited(t *testing.T, data []byte, l Limits) *Reader {
	t.Helper()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)), WithLimits(l))
	if err != nil {
		t.Fatalf("Open with %+v: %v", l, err)
	}
	return r
}

// TestDefaultLimits_KeepThePreviousCaps pins the defaults to the constants
// they replaced, so a Reader opened without [WithLimits] reads every document
// as it did before the budgets became configurable.
func TestDefaultLimits_KeepThePreviousCaps(t *testing.T) {
	t.Parallel()

	want := Limits{
		MaxStreamBytes:       256 << 20,
		MaxImagePixels:       100_000_000,
		MaxEmbeddedFileBytes: 50 * 1024 * 1024,
		MaxFormDepth:         10,
		MaxCachedObjects:     50_000,
		MaxRenderPixels:      50_000_000,
	}
	if got := DefaultLimits(); got != want {
		t.Errorf("DefaultLimits() = %+v, want %+v", got, want)
	}
	r := openLimited(t, minimalPDF(), Limits{})
	if got := limitsFromInternal(r.doc.Limits()); got != want {
		t.Errorf("a Reader opened with zero Limits reads under %+v, want %+v", got, want)
	}
}

// TestOpen_RefusesInvalidLimits pins [ErrInvalidLimits] for a negative budget
// and one past its ceiling, and accepts every budget at its ceiling.
func TestOpen_RefusesInvalidLimits(t *testing.T) {
	t.Parallel()

	data := minimalPDF()
	for name, l := range map[string]Limits{
		"negative stream bytes":         {MaxStreamBytes: -1},
		"negative image pixels":         {MaxImagePixels: -1},
		"negative embedded file bytes":  {MaxEmbeddedFileBytes: -1},
		"negative form depth":           {MaxFormDepth: -1},
		"negative cached objects":       {MaxCachedObjects: -1},
		"stream bytes past ceiling":     {MaxStreamBytes: 4<<30 + 1},
		"image pixels past ceiling":     {MaxImagePixels: 1<<30 + 1},
		"embedded bytes past ceiling":   {MaxEmbeddedFileBytes: 4<<30 + 1},
		"form depth past ceiling":       {MaxFormDepth: 257},
		"cached objects past ceiling":   {MaxCachedObjects: 8_388_608},
		"negative render pixels":        {MaxRenderPixels: -1},
		"render pixels past ceiling":    {MaxRenderPixels: 1<<30 + 1},
		"one bad field among good ones": {MaxStreamBytes: 1 << 20, MaxFormDepth: -3},
	} {
		if _, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)), WithLimits(l)); !errors.Is(err, ErrInvalidLimits) {
			t.Errorf("%s: Open error = %v, want ErrInvalidLimits", name, err)
		}
	}

	ceilings := Limits{
		MaxStreamBytes:       4 << 30,
		MaxImagePixels:       1 << 30,
		MaxEmbeddedFileBytes: 4 << 30,
		MaxFormDepth:         256,
		MaxCachedObjects:     8_388_607,
		MaxRenderPixels:      1 << 30,
	}
	for _, l := range []Limits{{}, DefaultLimits(), ceilings, {MaxFormDepth: 1, MaxCachedObjects: 1}} {
		openLimited(t, data, l)
	}
}

// TestLimitsValidate_IsTheCheckOpenRuns pins [Limits.Validate] to the check
// [Open] runs: for a budget below zero or past its ceiling it returns an error
// wrapping [ErrInvalidLimits] with the very message Open returns, naming the
// field, and for zero budgets, the defaults and the ceilings it returns nil.
func TestLimitsValidate_IsTheCheckOpenRuns(t *testing.T) {
	t.Parallel()

	data := minimalPDF()
	for name, tc := range map[string]struct {
		l     Limits
		field string
	}{
		"negative stream bytes":        {Limits{MaxStreamBytes: -1}, "MaxStreamBytes"},
		"image pixels past ceiling":    {Limits{MaxImagePixels: 1<<30 + 1}, "MaxImagePixels"},
		"embedded bytes past ceiling":  {Limits{MaxEmbeddedFileBytes: 4<<30 + 1}, "MaxEmbeddedFileBytes"},
		"form depth past ceiling":      {Limits{MaxFormDepth: 257}, "MaxFormDepth"},
		"negative cached objects":      {Limits{MaxCachedObjects: -1}, "MaxCachedObjects"},
		"render pixels past ceiling":   {Limits{MaxRenderPixels: 1<<30 + 1}, "MaxRenderPixels"},
		"the first bad field is named": {Limits{MaxStreamBytes: 4<<30 + 1, MaxFormDepth: -1}, "MaxStreamBytes"},
	} {
		verr := tc.l.Validate()
		if !errors.Is(verr, ErrInvalidLimits) {
			t.Errorf("%s: Validate() = %v, want ErrInvalidLimits", name, verr)
			continue
		}
		if !strings.Contains(verr.Error(), tc.field) {
			t.Errorf("%s: Validate() = %q, want it to name %s", name, verr, tc.field)
		}
		_, oerr := Open(context.Background(), bytes.NewReader(data), int64(len(data)), WithLimits(tc.l))
		if oerr == nil || oerr.Error() != verr.Error() {
			t.Errorf("%s: Open error = %v, want Validate's %q", name, oerr, verr)
		}
	}

	ceilings := Limits{
		MaxStreamBytes:       4 << 30,
		MaxImagePixels:       1 << 30,
		MaxEmbeddedFileBytes: 4 << 30,
		MaxFormDepth:         256,
		MaxCachedObjects:     8_388_607,
		MaxRenderPixels:      1 << 30,
	}
	for _, l := range []Limits{{}, DefaultLimits(), ceilings} {
		if err := l.Validate(); err != nil {
			t.Errorf("Validate(%+v) = %v, want nil", l, err)
		}
	}
}

// budgetImagesPDF builds a page showing three 1024x1024 grey images, each
// decoding to 1 MiB of samples: /Flate through FlateDecode with a PNG
// predictor, whose FlateDecode stage yields 1,049,600 bytes, /RL through
// RunLengthDecode at its greatest expansion, and /Raw unfiltered.
func budgetImagesPDF(t *testing.T) []byte {
	t.Helper()

	const side = 1024
	var rows bytes.Buffer
	for range side {
		rows.WriteByte(0) // PNG predictor None
		rows.Write(make([]byte, side))
	}
	var flate bytes.Buffer
	w := zlib.NewWriter(&flate)
	if _, err := w.Write(rows.Bytes()); err != nil {
		t.Fatalf("zlib write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}
	var rl bytes.Buffer
	for range side * side / 128 {
		rl.Write([]byte{0x81, 0x00})
	}
	rl.WriteByte(0x80)

	image := fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 8", side, side)
	return limitsPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R"+
			" /Resources << /XObject << /Flate 5 0 R /RL 6 0 R /Raw 7 0 R >> >> >>",
		streamObj("", []byte("/Flate Do /RL Do /Raw Do")),
		streamObj(image+fmt.Sprintf(" /Filter /FlateDecode /DecodeParms << /Predictor 12 /Columns %d >>", side), flate.Bytes()),
		streamObj(image+" /Filter /RunLengthDecode", rl.Bytes()),
		streamObj(image, make([]byte, side*side)),
	)
}

// TestLimits_StreamBudget pins [Limits.MaxStreamBytes] through the public API:
// a FlateDecode image and a RunLengthDecode image that decode under the default
// budget fail with [ErrStreamTooLarge] under a 512 KiB one, from
// [Reader.DecodeImage] and from the image's Open function.
func TestLimits_StreamBudget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := budgetImagesPDF(t)
	def := openLimited(t, data, Limits{})
	low := openLimited(t, data, Limits{MaxStreamBytes: 512 << 10})
	for _, name := range []string{"Flate", "RL"} {
		if _, err := def.DecodeImage(ctx, 1, name); err != nil {
			t.Errorf("%s under the default budget: %v", name, err)
		}
		_, err := low.DecodeImage(ctx, 1, name)
		if !errors.Is(err, ErrStreamTooLarge) || !errors.Is(err, ErrImageNotDecodable) {
			t.Errorf("%s under a 512 KiB budget: err = %v, want ErrStreamTooLarge and ErrImageNotDecodable", name, err)
		}
	}

	metas, err := low.ExtractImages(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractImages: %v", err)
	}
	opened := false
	for _, m := range metas {
		if m.Filename != "RL" {
			continue
		}
		opened = true
		rc, err := m.Open(ctx)
		if err == nil {
			_, err = io.Copy(io.Discard, rc)
			rc.Close()
		}
		if !errors.Is(err, ErrStreamTooLarge) {
			t.Errorf("Open of /RL under a 512 KiB budget: err = %v, want ErrStreamTooLarge", err)
		}
	}
	if !opened {
		t.Error("the listing holds no /RL image")
	}
}

// heavyPagePDF returns a one-page document whose /Contents array holds two
// 20 KiB parts, each well inside a 64 KiB stream budget, whose sum passes a
// quarter of it.
func heavyPagePDF() []byte {
	const partSize = 20 << 10
	part := []byte("BT /F1 12 Tf 72 700 Td (Heavy) Tj ET\n")
	part = append(part, bytes.Repeat([]byte(" "), partSize-len(part))...)
	return limitsPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents [4 0 R 5 0 R] /Resources << /Font << /F1 6 0 R >> >> >>",
		streamObj("", part),
		streamObj("", part),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	)
}

// TestLimits_PageContentFollowsTheStreamBudget pins the page content bound to
// [Limits.MaxStreamBytes]: the page of [heavyPagePDF] is refused with
// [ErrPageContentTooLarge] under a 64 KiB stream budget, since its two parts
// sum past a quarter of it, and read under the default and under a 256 KiB
// budget.
func TestLimits_PageContentFollowsTheStreamBudget(t *testing.T) {
	t.Parallel()

	data := heavyPagePDF()

	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		l     Limits
		reads bool
	}{
		{"default", Limits{}, true},
		{"256 KiB", Limits{MaxStreamBytes: 256 << 10}, true},
		{"64 KiB", Limits{MaxStreamBytes: 64 << 10}, false},
	} {
		text, err := openLimited(t, data, tc.l).ExtractText(ctx, 1)
		if tc.reads {
			if err != nil || !strings.Contains(text, "Heavy") {
				t.Errorf("%s: ExtractText = %q, %v; want the page read", tc.name, text, err)
			}
			continue
		}
		if !errors.Is(err, ErrPageContentTooLarge) {
			t.Errorf("%s: ExtractText = %q, %v; want %v", tc.name, text, err, ErrPageContentTooLarge)
		}
	}
}

// TestLimits_PageContentTooLargeReachesTheWriter pins that a Writer operation
// refusing a page past the page content bound says so with
// [ErrPageContentTooLarge]: [Writer.Redact], alongside
// [ErrRedactionRefused], and [Writer.WriteTo] for a [Writer.StampPDF] whose
// source page is the heavy one. The control stamps and redacts under the
// default budget.
func TestLimits_PageContentTooLargeReachesTheWriter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, tc := range []struct {
		name    string
		l       Limits
		refused bool
	}{
		{"default", Limits{}, false},
		{"64 KiB", Limits{MaxStreamBytes: 64 << 10}, true},
	} {
		heavy := openLimited(t, heavyPagePDF(), tc.l)
		_, err := NewWriter(heavy).Redact(ctx, []RedactRegion{{Page: 1, Rect: [4]float64{70, 690, 200, 720}}})
		if tc.refused != errors.Is(err, ErrPageContentTooLarge) || (tc.refused && !errors.Is(err, ErrRedactionRefused)) || (!tc.refused && err != nil) {
			t.Errorf("%s: Redact error = %v, want the page refused %v", tc.name, err, tc.refused)
		}

		w := NewWriter(openLimited(t, minimalPDF(), tc.l))
		if err := w.StampPDF(PDFStamp{Source: openLimited(t, heavyPagePDF(), tc.l)}); err != nil {
			t.Fatalf("%s: StampPDF: %v", tc.name, err)
		}
		err = w.WriteTo(ctx, io.Discard)
		if tc.refused != errors.Is(err, ErrPageContentTooLarge) || (!tc.refused && err != nil) {
			t.Errorf("%s: WriteTo of a heavy stamp = %v, want the page refused %v", tc.name, err, tc.refused)
		}
	}
}

// TestLimits_ImagePixelBudget pins [Limits.MaxImagePixels]: the 1024x1024
// image decodes under a budget of exactly its pixels and fails with
// [ErrImageTooLarge] under one pixel less.
func TestLimits_ImagePixelBudget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := budgetImagesPDF(t)
	if _, err := openLimited(t, data, Limits{MaxImagePixels: 1024 * 1024}).DecodeImage(ctx, 1, "Raw"); err != nil {
		t.Errorf("under a budget of its pixels: %v", err)
	}
	_, err := openLimited(t, data, Limits{MaxImagePixels: 1024*1024 - 1}).DecodeImage(ctx, 1, "Raw")
	if !errors.Is(err, ErrImageTooLarge) || !errors.Is(err, ErrImageNotDecodable) {
		t.Errorf("under a budget one pixel short: err = %v, want ErrImageTooLarge and ErrImageNotDecodable", err)
	}
}

// TestLimits_EmbeddedFileBudget pins [Limits.MaxEmbeddedFileBytes]: an
// embedded PDF that is no PDF/A file raises ISO 19005-2, 6.8 t5 under the
// default budget, and a budget below its size leaves it unvalidated.
func TestLimits_EmbeddedFileBudget(t *testing.T) {
	t.Parallel()

	inner := minimalPDF()
	data := limitsPDF(
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Names [(inner.pdf) 4 0 R] >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Filespec /F (inner.pdf) /UF (inner.pdf) /EF << /F 5 0 R >> >>",
		streamObj("/Type /EmbeddedFile /Subtype /application#2Fpdf", inner),
	)
	count := func(l Limits) int {
		violations, err := openLimited(t, data, l).ValidatePDFAReport(context.Background(), models.PDFA2b)
		if err != nil {
			t.Fatalf("ValidatePDFAReport under %+v: %v", l, err)
		}
		n := 0
		for _, v := range violations {
			if v.Clause == "6.8" && v.TestNumber == 5 {
				n++
			}
		}
		return n
	}
	if n := count(Limits{}); n != 1 {
		t.Errorf("default budget: %d 6.8 t5 violations, want 1", n)
	}
	if n := count(Limits{MaxEmbeddedFileBytes: int64(len(inner)) - 1}); n != 0 {
		t.Errorf("budget below the embedded PDF: %d 6.8 t5 violations, want 0", n)
	}
}

// nestedFormsPDF builds a page that paints /Fm1, which paints /Fm2, which
// paints /Fm3, which shows the text "Deep" and the image /Deep.
func nestedFormsPDF() []byte {
	form := func(res string, content string) string {
		return streamObj("/Type /XObject /Subtype /Form /BBox [0 0 612 792] /Resources << "+res+" >>", []byte(content))
	}
	return limitsPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /XObject << /Fm1 5 0 R >> >> >>",
		streamObj("", []byte("/Fm1 Do")),
		form("/XObject << /Fm2 6 0 R >>", "/Fm2 Do"),
		form("/XObject << /Fm3 7 0 R >>", "/Fm3 Do"),
		form("/XObject << /Deep 8 0 R >> /Font << /F1 9 0 R >>", "BT /F1 12 Tf 72 700 Td (Deep) Tj ET q 10 0 0 10 72 600 cm /Deep Do Q"),
		streamObj("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", []byte{0}),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	)
}

// TestLimits_FormDepth pins [Limits.MaxFormDepth] for text extraction and image
// listing alike: what /Fm3, three forms down, shows is read under a budget of
// 3 and the default, and skipped under a budget of 2.
func TestLimits_FormDepth(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := nestedFormsPDF()
	for _, tc := range []struct {
		depth int
		reads bool
	}{{0, true}, {3, true}, {2, false}, {1, false}} {
		r := openLimited(t, data, Limits{MaxFormDepth: tc.depth})
		text, err := r.ExtractText(ctx, 1)
		if err != nil && !errors.Is(err, ErrRequiresOCR) {
			t.Fatalf("depth %d: ExtractText: %v", tc.depth, err)
		}
		if got := strings.Contains(text, "Deep"); got != tc.reads {
			t.Errorf("depth %d: text %q holds \"Deep\" = %v, want %v", tc.depth, text, got, tc.reads)
		}
		metas, err := r.ExtractImages(ctx, 1)
		if err != nil {
			t.Fatalf("depth %d: ExtractImages: %v", tc.depth, err)
		}
		if got := len(metas) == 1 && metas[0].Filename == "Deep"; got != tc.reads {
			t.Errorf("depth %d: listing %+v holds /Deep = %v, want %v", tc.depth, metas, got, tc.reads)
		}
	}
}

// TestLimits_CachedObjectsStillRead pins that [Limits.MaxCachedObjects] costs
// re-parsing, never a wrong answer: a cache of one object reads every page of
// a document as the default cache does.
func TestLimits_CachedObjectsStillRead(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := nestedFormsPDF()
	def := openLimited(t, data, Limits{})
	one := openLimited(t, data, Limits{MaxCachedObjects: 1})
	want, err := def.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("default cache: %v", err)
	}
	for range 3 {
		got, err := one.ExtractText(ctx, 1)
		if err != nil || got != want {
			t.Errorf("a one-object cache read %q, %v; want %q", got, err, want)
		}
	}
}

// TestReader_ReopenCarriesLimits pins that a document litho re-reads from a
// Reader, as the remediation gates and every pipeline stage do, is read under
// the Reader's budgets rather than the defaults.
func TestReader_ReopenCarriesLimits(t *testing.T) {
	t.Parallel()

	data := nestedFormsPDF()
	l := Limits{MaxStreamBytes: 1 << 20, MaxImagePixels: 1 << 16, MaxEmbeddedFileBytes: 1 << 10, MaxFormDepth: 2, MaxCachedObjects: 7, MaxRenderPixels: 1 << 20}
	r := openLimited(t, data, l)
	path := filepath.Join(t.TempDir(), "reopen.pdf")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	next, err := r.reopenFile(context.Background(), path)
	if err != nil {
		t.Fatalf("reopenFile: %v", err)
	}
	defer next.Close()
	if got := limitsFromInternal(next.doc.Limits()); got != l {
		t.Errorf("the re-read document reads under %+v, want %+v", got, l)
	}
}
