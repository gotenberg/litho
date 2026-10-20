package mutations

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/heuristics"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/models"
)

// splitHelvetica is the font resource of most split fixtures: Helvetica, whose
// widths litho knows without a /Widths array.
const splitHelvetica = "4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n"

// splitType3 is a Type 3 font drawing "a" and "b" as filled squares 500 glyph
// units wide, the second advancing 700.
const splitType3 = "4 0 obj\n<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000]" +
	" /FontMatrix [0.001 0 0 0.001 0 0] /FirstChar 97 /LastChar 98 /Widths [500 700]" +
	" /Encoding << /Type /Encoding /Differences [97 /a /b] >>" +
	" /CharProcs << /a 7 0 R /b 7 0 R >> >>\nendobj\n" +
	"7 0 obj\n<< /Length 33 >>\nstream\n500 0 0 0 500 500 d1 0 0 500 500 re f\nendstream\nendobj\n"

// splitCID returns a Type 0 font under Identity-H whose /ToUnicode CMap
// declares codespace, mapping <0041> to <0046> onto "A" to "F" (or, for a
// one-byte codespace, <41> to <46>).
func splitCID(codespace string) string {
	cmap := "/CIDInit /ProcSet findresource begin 12 dict begin begincmap " +
		"1 begincodespacerange " + codespace + " endcodespacerange "
	if strings.HasPrefix(codespace, "<00>") {
		cmap += "1 beginbfrange <41> <46> <0041> endbfrange "
	} else {
		cmap += "1 beginbfrange <0041> <0046> <0041> endbfrange "
	}
	cmap += "endcmap CMapName currentdict /CMap defineresource pop end end"
	return "4 0 obj\n<< /Type /Font /Subtype /Type0 /BaseFont /Split /Encoding /Identity-H" +
		" /DescendantFonts [8 0 R] /ToUnicode 9 0 R >>\nendobj\n" +
		"8 0 obj\n<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Split" +
		" /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >>" +
		" /FontDescriptor 10 0 R /DW 600 /W [65 [500 550 600 650 700 750]] /CIDToGIDMap /Identity >>\nendobj\n" +
		fmt.Sprintf("9 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", len(cmap), cmap) +
		"10 0 obj\n<< /Type /FontDescriptor /FontName /Split /Flags 4 /FontBBox [0 -200 1000 900]" +
		" /ItalicAngle 0 /Ascent 900 /Descent -200 /CapHeight 700 /StemV 80 >>\nendobj\n"
}

// splitDoc builds a one-page document whose resources name font object 4,
// defined by fontObjs, as /F1 and whose content stream is body.
func splitDoc(t *testing.T, fontObjs, body string) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()

	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		fontObjs +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		fmt.Sprintf("6 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", len(body), body) +
		"trailer\n<< /Size 11 /Root 1 0 R >>\n%%EOF\n")

	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	return doc, pages[0]
}

// splitGlyphs extracts the page's glyphs, one element each.
func splitGlyphs(t *testing.T, doc *document.Document, page *document.Page, body []byte) []models.TextElement {
	t.Helper()
	ctx := context.Background()
	ops, err := content.ParseStreamData(ctx, body)
	if err != nil {
		t.Fatalf("parsing %q: %v", body, err)
	}
	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	elements, err := typography.ExtractElements(ctx, doc, machine, page, ops)
	if err != nil {
		t.Fatalf("ExtractElements: %v", err)
	}
	return elements
}

// assertSplit redacts region from a page drawing body in fontObjs, and checks
// the contract of a split: every glyph whose box meets the region is gone, and
// every other glyph is still drawn, with the same text at the same position. It
// returns the rewritten content and the report.
func assertSplit(t *testing.T, fontObjs, body string, region [4]float64) ([]byte, RedactReport) {
	t.Helper()
	doc, page := splitDoc(t, fontObjs, body)
	before := splitGlyphs(t, doc, page, []byte(body))

	zone := heuristics.ZoneFromRect(region)
	var want []models.TextElement
	inside := 0
	for _, el := range before {
		if heuristics.ZonesOverlap(heuristics.ElementZone(el), zone) {
			inside++
			continue
		}
		want = append(want, el)
	}
	if inside == 0 || len(want) == 0 {
		t.Fatalf("fixture: the region covers %d of %d glyphs, want some but not all", inside, len(before))
	}

	alloc := writer.NewObjAllocator(20)
	report, err := RedactPageRegions(context.Background(), alloc, doc, page, [][4]float64{region})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	got := newPageContents(t, alloc, page.Ref.ObjectNum)

	redone, redonePage := splitDoc(t, fontObjs, string(got))
	after := splitGlyphs(t, redone, redonePage, got)
	if len(after) != len(want) {
		t.Fatalf("drew %d glyphs after the split, want the %d outside the region\nbefore: %s\nafter: %s",
			len(after), len(want), body, got)
	}
	for k := range want {
		w, g := want[k], after[k]
		if w.Text != g.Text || math.Abs(w.X-g.X) > 1e-3 || math.Abs(w.Y-g.Y) > 1e-3 {
			t.Errorf("glyph %d: got %q at (%.4f, %.4f), want %q at (%.4f, %.4f)\nafter: %s",
				k, g.Text, g.X, g.Y, w.Text, w.X, w.Y, got)
		}
	}
	for _, el := range after {
		if heuristics.ZonesOverlap(heuristics.ElementZone(el), zone) {
			t.Errorf("glyph %q at (%.2f, %.2f) is still inside the region", el.Text, el.X, el.Y)
		}
	}
	return got, report
}

// assertSplitRefused checks that a region over part of an operator in body is
// refused with ErrPartialOperatorCoverage and leaves nothing queued.
func assertSplitRefused(t *testing.T, fontObjs, body string, region [4]float64) {
	t.Helper()
	doc, page := splitDoc(t, fontObjs, body)
	alloc := writer.NewObjAllocator(20)
	_, err := RedactPageRegions(context.Background(), alloc, doc, page, [][4]float64{region})
	if !errors.Is(err, ErrPartialOperatorCoverage) {
		t.Fatalf("err = %v, want ErrPartialOperatorCoverage", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects while refusing", n)
	}
}

// coversSecretHead covers the first two glyphs of "SECRET" drawn at (100, 700)
// in 12-point Helvetica: S ends at 108 and E at 116.
var coversSecretHead = [4]float64{90, 690, 110, 730}

func TestSplit_Tj(t *testing.T) {
	got, report := assertSplit(t, splitHelvetica,
		"BT /F1 12 Tf 100 700 Td (SECRET) Tj ET", coversSecretHead)
	if bytes.Contains(got, []byte("SE")) {
		t.Errorf("the covered glyphs are still in the content: %s", got)
	}
	if len(report.RemovedText) != 1 || report.RemovedText[0] != "SE" {
		t.Errorf("RemovedText = %q, want [SE]", report.RemovedText)
	}
}

func TestSplit_TjMiddleKeepsBothSides(t *testing.T) {
	// C runs 116 to 124.67 and R from 124.67: a region over C alone.
	got, report := assertSplit(t, splitHelvetica,
		"BT /F1 12 Tf 100 700 Td (SECRET) Tj ET", [4]float64{118, 690, 122, 730})
	if !bytes.Contains(got, []byte("(SE)")) || !bytes.Contains(got, []byte("(RET)")) {
		t.Errorf("the kept runs are not both in the content: %s", got)
	}
	if len(report.RemovedText) != 1 || report.RemovedText[0] != "C" {
		t.Errorf("RemovedText = %q, want [C]", report.RemovedText)
	}
}

func TestSplit_TrailingRunLeavesNoWidth(t *testing.T) {
	// Nothing after the run depends on the pen, so the removed tail must not
	// leave a number recording its width.
	got, _ := assertSplit(t, splitHelvetica,
		"BT /F1 12 Tf 100 700 Td (SECRET) Tj ET", [4]float64{130, 690, 200, 730})
	if !bytes.Contains(got, []byte("[(SEC)] TJ")) {
		t.Errorf("the split did not end at the kept glyphs: %s", got)
	}
}

func TestSplit_TrailingRunKeepsThePenForLaterText(t *testing.T) {
	// A Tj after the split starts where the split operator ended, so the
	// removed tail's width has to be put back.
	assertSplit(t, splitHelvetica,
		"BT /F1 12 Tf 100 700 Td (SECRET) Tj (KEEP) Tj ET", [4]float64{130, 690, 145, 730})
}

func TestSplit_TJFoldsTheKerningAroundRemovedGlyphs(t *testing.T) {
	got, _ := assertSplit(t, splitHelvetica,
		"BT /F1 12 Tf 100 700 Td [(SE) -137 (CR) 61 (ET)] TJ ET", [4]float64{118, 690, 122, 730})
	// -137 sits between E and C, and C is removed: a kept kerning number
	// beside a removed glyph depends on that glyph.
	if bytes.Contains(got, []byte("-137")) {
		t.Errorf("a kerning number touching a removed glyph survived: %s", got)
	}
}

func TestSplit_TJKeepsKerningBetweenKeptGlyphs(t *testing.T) {
	got, _ := assertSplit(t, splitHelvetica,
		"BT /F1 12 Tf 100 700 Td [(S) -250 (ECRET)] TJ ET", [4]float64{150, 690, 200, 730})
	if !bytes.Contains(got, []byte("-250")) {
		t.Errorf("a kerning number between kept glyphs was lost: %s", got)
	}
}

func TestSplit_SpacingAndRise(t *testing.T) {
	// Character and word spacing and a rise all move the kept glyphs; the
	// removed space carries the word spacing.
	assertSplit(t, splitHelvetica,
		"BT /F1 12 Tf 1.5 Tc 4 Tw 3 Ts 100 700 Td (SE CRET) Tj ET", [4]float64{120, 690, 135, 730})
}

func TestSplit_ScalingWithoutANumber(t *testing.T) {
	// Under 130 Tz a removed tail needs no number, so the split stands.
	assertSplit(t, splitHelvetica,
		"BT /F1 12 Tf 1.5 Tc 4 Tw 130 Tz 100 700 Td (SE CRET) Tj ET", [4]float64{150, 690, 220, 730})
}

func TestSplit_RefusesANumberUnderScaling(t *testing.T) {
	// The removed middle needs a number, which 9.4.4 scales by Th and the
	// extractor does not, so the rewrite could not be verified.
	assertSplitRefused(t, splitHelvetica,
		"BT /F1 12 Tf 130 Tz 100 700 Td (SECRET) Tj ET", [4]float64{120, 690, 124, 730})
}

func TestSplit_NegativeKerningUnderSpacing(t *testing.T) {
	assertSplit(t, splitHelvetica,
		"BT /F1 12 Tf 0.7 Tc 3 Tw 100 700 Td [(SE) 200 ( CR) -90 (ET)] TJ ET", [4]float64{118, 690, 124, 730})
}

func TestSplit_Quote(t *testing.T) {
	got, _ := assertSplit(t, splitHelvetica,
		"BT /F1 12 Tf 14 TL 100 714 Td (SECRET) ' ET", coversSecretHead)
	if !bytes.Contains(got, []byte("T*")) {
		t.Errorf("the line move of ' was lost: %s", got)
	}
}

func TestSplit_DoubleQuote(t *testing.T) {
	got, _ := assertSplit(t, splitHelvetica,
		"BT /F1 12 Tf 14 TL 100 714 Td 2 1 (SE CRET) \" (KEEP) Tj ET", [4]float64{100, 690, 112, 730})
	for _, want := range []string{"2 Tw", "1 Tc", "T*"} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("%q, which \" set, was lost: %s", want, got)
		}
	}
}

func TestSplit_InvisibleText(t *testing.T) {
	// Mode 3 paints nothing and clips nothing; its text is still text.
	assertSplit(t, splitHelvetica,
		"BT 3 Tr /F1 12 Tf 100 700 Td (SECRET) Tj ET", coversSecretHead)
}

func TestSplit_Type3(t *testing.T) {
	// a advances 6 points and b 8.4 at 12 points: the region meets the
	// second glyph alone.
	assertSplit(t, splitType3,
		"BT /F1 12 Tf 100 700 Td (abab) Tj ET", [4]float64{108, 690, 112, 730})
}

func TestSplit_CIDSplitsOnTwoByteCodes(t *testing.T) {
	// <0041> A is 6 points, <0042> B 6.6, <0043> C 7.2: the region meets B.
	got, _ := assertSplit(t, splitCID("<0000> <FFFF>"),
		"BT /F1 12 Tf 100 700 Td <004100420043> Tj ET", [4]float64{108, 690, 110, 730})
	if bytes.Contains(got, []byte("\x00B")) {
		t.Errorf("the removed code is still in the content: %q", got)
	}
}

func TestSplit_RefusesACodeSplitAgainstTheEncoding(t *testing.T) {
	// The /ToUnicode codespace reads one byte at a time while Identity-H
	// reads two, so the extractor's glyph boundaries are not the codes a
	// viewer draws: cutting at them could keep half a code.
	assertSplitRefused(t, splitCID("<00> <FF>"),
		"BT /F1 12 Tf 100 700 Td (ABCDEF) Tj ET", [4]float64{100, 690, 104, 730})
}

func TestSplit_RefusesClippingText(t *testing.T) {
	// Mode 7 adds every glyph to the clipping path, so removing one changes
	// what later content shows outside the region.
	assertSplitRefused(t, splitHelvetica,
		"BT 7 Tr /F1 12 Tf 100 700 Td (SECRET) Tj ET", coversSecretHead)
}

func TestSplit_RefusesKerningUnderScaling(t *testing.T) {
	// The extractor moves a TJ number without Th, so under 50 Tz the boxes
	// after it are not where a viewer draws them.
	assertSplitRefused(t, splitHelvetica,
		"BT /F1 12 Tf 50 Tz 100 700 Td [(SE) -500 (CRET)] TJ ET", [4]float64{90, 690, 104, 730})
}

func TestSplit_MarksMarkedContentOnBothSides(t *testing.T) {
	doc, page := splitDoc(t, splitHelvetica,
		"/P <</MCID 0>> BDC BT /F1 12 Tf 100 700 Td (SECRET) Tj ET EMC")
	alloc := writer.NewObjAllocator(20)
	report, err := RedactPageRegions(context.Background(), alloc, doc, page, [][4]float64{coversSecretHead})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if len(report.RemovedMCIDs) != 1 || report.RemovedMCIDs[0] != 0 {
		t.Errorf("RemovedMCIDs = %v, want [0]", report.RemovedMCIDs)
	}
}
