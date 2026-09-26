package compliance

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// pagePaintsTransparency reports whether page, of doc, contains transparency
// under the PDF/A part part, from its content and its annotations, as
// [checkTransparencyParts2Plus] asks it.
func pagePaintsTransparency(ctx context.Context, doc *document.Document, page ast.PdfDict, part int) bool {
	profile := "PDF/A-2b"
	if part == 4 {
		profile = "PDF/A-4"
	}
	pages := pageCtxsForPages(ctx, doc, page)
	w := newPaintTransparency(&validationCtx{doc: doc, profile: profile, part: part, pages: pages})
	return w.pageTransparent(ctx, &pages[0]) || w.annotationsTransparent(ctx, &pages[0], part)
}

// paintProbe builds a one-page document whose page paints data under a
// resource dictionary holding the graphics states, XObjects, patterns, colour
// spaces, fonts and shading the table below uses, and reports whether the page
// contains transparency.
func paintProbe(t *testing.T, data string, annotAP string) bool {
	t.Helper()
	ctx := context.Background()
	form := func(body, extra string) string {
		return fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 50 50] %s /Length %d >>\nstream\n%s\nendstream", extra, len(body), body)
	}
	image := func(extra string) string {
		return "<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceGray " +
			extra + " /Length 1 >>\nstream\n\x80\nendstream"
	}
	fca := "/Resources << /ExtGState << /ca << /ca 0.5 >> >> >>"
	annots := ""
	if annotAP != "" {
		annots = "/Annots [<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /AP << /N 21 0 R >> >>]"
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R /Resources 5 0 R " + annots + " >>",
		streamObj(data),
		"<< /ExtGState << /ca << /ca 0.5 >> /CA << /CA 0.5 >> /ca15 << /ca 1.5 >> /ca1 << /ca 1 >> /lw << /LW 2 >>" +
			" /sm << /SMask << /S /Luminosity /G 6 0 R >> >> /smnone << /SMask /None >> /bmmul << /BM /Multiply >>" +
			" /bmnorm << /BM /Normal >> /bmcompat << /BM /Compatible >> /bmnormarr << /BM [/Normal] >>" +
			" /bmnormmul << /BM [/Normal /Multiply] >> /bmnum << /BM 5 >> /caname << /ca /Foo >> /caref << /ca 7 0 R >>" +
			" /canear7 << /ca 0.9999999 >> /canear8 << /ca 0.99999999 >> >>" +
			" /XObject << /Fo 8 0 R /Fs 9 0 R /Fca 10 0 R /Fnp 11 0 R /Fgrp 12 0 R /Im 13 0 R /ImSm 14 0 R" +
			" /ImNone 15 0 R /ImInd 16 0 R /Cyc 17 0 R >>" +
			" /Pattern << /P1 18 0 R /P2 25 0 R /Sp 19 0 R >> /ColorSpace << /Cs0 [/Pattern /DeviceGray] >>" +
			" /Font << /T3 20 0 R >> /Shading << /Sh << /ShadingType 2 /ColorSpace /DeviceGray /Coords [0 0 10 0]" +
			" /Function << /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >> >> >> >>",
		form("0 g 0 0 10 10 re f", "/Group << /S /Transparency /CS /DeviceGray >>"),
		"0.5",
		form("0 0 10 10 re f", ""),
		form("0 0 10 10 re S", ""),
		form("/ca gs 0 0 10 10 re f", fca),
		form("/ca gs", fca),
		form("", "/Group << /S /Transparency /CS /DeviceRGB >>"),
		image(""),
		image("/SMask 13 0 R"),
		image("/SMask /None"),
		image("/SMaskInData 1"),
		form("/Cyc Do 0 0 10 10 re f", "/Resources << /XObject << /Cyc 17 0 R >> >>"),
		"<< /Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 " + fca +
			" /Length 19 >>\nstream\n/ca gs 0 0 5 5 re f\nendstream",
		"<< /Type /Pattern /PatternType 2 /Shading << /ShadingType 2 /ColorSpace /DeviceGray /Coords [0 0 10 0]" +
			" /Function << /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >> >> >>",
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0]" +
			" /CharProcs << /a 22 0 R /b 23 0 R /c 24 0 R >> /Encoding << /Differences [65 /a /b /c 200 /a] >>" +
			" /FirstChar 65 /LastChar 200 /Widths [" + strings.Repeat("1000 ", 136) + "] " + fca + " >>",
		annotAP,
		streamObj("1000 0 d0 /ca gs 0 0 500 500 re f"),
		streamObj("1000 0 d0 0 0 500 500 re f"),
		streamObj("1000 0 0 0 1000 1000 d1 /ca gs 0 0 500 500 re f"),
		"<< /Type /Pattern /PatternType 1 /PaintType 2 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 " + fca +
			" /Length 19 >>\nstream\n/ca gs 0 0 5 5 re f\nendstream",
	}
	if annotAP == "" {
		objs[20] = "null"
	}
	doc, _ := objsDocPDF(t, objs...)
	page, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil {
		t.Fatalf("resolve page: %v", err)
	}
	return pagePaintsTransparency(ctx, doc, page, 2)
}

// TestPaintTransparencyAtThePaintingOperator pins that a page contains
// transparency, for ISO 19005-2, 6.2.10 t2 and ISO 19005-4, 6.2.9 t2, only
// where an operator paints under it, as veraPDF 1.30 reads it at PDF/A-2b and
// PDF/A-4 (see [paintTransparency]). Every case was measured on a page of its
// own with no group and no output intent.
func TestPaintTransparencyAtThePaintingOperator(t *testing.T) {
	t.Parallel()
	appearance := func(body, extra string) string {
		return fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] %s /Length %d >>\nstream\n%s\nendstream", extra, len(body), body)
	}
	tests := []struct {
		name, data, annot string
		want              bool
	}{
		{"ca undone by Q before a fill", "q /ca gs Q 0 0 10 10 re f", "", false},
		{"ca before a stroke", "/ca gs 0 0 10 10 re S", "", false},
		{"CA before a fill", "/CA gs 0 0 10 10 re f", "", false},
		{"ca 1.5 before a fill", "/ca15 gs 0 0 10 10 re f", "", false},
		{"ca before a fill", "/ca gs 0 0 10 10 re f", "", true},
		{"CA before a stroke", "/CA gs 0 0 10 10 re S", "", true},
		{"ca and no paint", "/ca gs", "", false},
		{"ca before an opaque form", "/ca gs /Fo Do", "", true},
		{"CA before a stroking form", "/CA gs /Fs Do", "", false},
		{"form filling at ca", "/Fca Do", "", true},
		{"form setting ca and painting nothing", "/Fnp Do", "", false},
		{"soft mask before n", "/sm gs 0 0 10 10 re n", "", false},
		{"soft mask before a fill", "/sm gs 0 0 10 10 re f", "", true},
		{"blend mode before invisible text", "/bmmul gs BT /T3 12 Tf 3 Tr (B) Tj ET", "", false},
		{"blend mode before filled text", "/bmmul gs BT /T3 12 Tf 0 Tr (B) Tj ET", "", true},
		{"ca before sh", "/ca gs /Sh sh", "", true},
		{"CA before sh", "/CA gs /Sh sh", "", false},
		{"ca before an inline image", "/ca gs BI /W 1 /H 1 /CS /G /BPC 8 ID \x80 EI", "", false},
		{"CA before an image", "/CA gs /Im Do", "", false},
		{"soft masked image", "/ImSm Do", "", true},
		{"transparent pattern filled", "/Pattern cs /P1 scn 0 0 10 10 re f", "", true},
		{"transparent pattern selected only", "/Pattern cs /P1 scn", "", false},
		{"glyph filling at ca shown", "BT /T3 12 Tf (A) Tj ET", "", true},
		{"opaque glyph shown", "BT /T3 12 Tf (B) Tj ET", "", false},
		{"ca reset by ca 1", "/ca gs /ca1 gs 0 0 10 10 re f", "", false},
		{"ca kept by a state without ca", "/ca gs /lw gs 0 0 10 10 re f", "", true},
		{"BM Compatible", "/bmcompat gs 0 0 10 10 re f", "", true},
		{"BM [/Normal]", "/bmnormarr gs 0 0 10 10 re f", "", false},
		{"BM [/Normal /Multiply]", "/bmnormmul gs 0 0 10 10 re f", "", true},
		{"BM 5", "/bmnum gs 0 0 10 10 re f", "", true},
		{"SMask None", "/smnone gs 0 0 10 10 re f", "", false},
		{"ca before a clip", "/ca gs 0 0 10 10 re W n", "", false},
		{"transparency group painting nothing", "/Fgrp Do", "", true},
		{"soft mask cleared by a state without one", "/sm gs /lw gs 0 0 10 10 re f", "", false},
		{"blend mode cleared by a state without one", "/bmmul gs /lw gs 0 0 10 10 re f", "", false},
		{"blend mode cleared by Normal", "/bmmul gs /bmnorm gs 0 0 10 10 re f", "", false},
		{"ca kept by an undefined state", "/ca gs /Nope gs 0 0 10 10 re f", "", true},
		{"ca written as a name", "/caname gs 0 0 10 10 re f", "", false},
		{"ca written as a reference", "/caref gs 0 0 10 10 re f", "", true},
		{"ca 0.9999999", "/canear7 gs 0 0 10 10 re f", "", true},
		{"ca 0.99999999", "/canear8 gs 0 0 10 10 re f", "", false},
		{"ca before fill-and-clip text", "/ca gs BT /T3 12 Tf 4 Tr (B) Tj ET", "", true},
		{"ca before clip text", "/ca gs BT /T3 12 Tf 7 Tr (B) Tj ET", "", false},
		{"ca before stroked text", "/ca gs BT /T3 12 Tf 1 Tr (B) Tj ET", "", false},
		{"CA before stroked text", "/CA gs BT /T3 12 Tf 1 Tr (B) Tj ET", "", true},
		{"form painting itself", "/Cyc Do", "", false},
		{"stroking pattern stroked", "/Pattern CS /P1 SCN 0 0 10 10 re S", "", true},
		{"stroking pattern with a fill", "/Pattern CS /P1 SCN 0 0 10 10 re f", "", false},
		{"pattern inherited by an opaque form", "/Pattern cs /P1 scn /Fo Do", "", true},
		{"pattern filling text", "/Pattern cs /P1 scn BT /T3 12 Tf (B) Tj ET", "", true},
		{"glyph shown at code 200", "BT /T3 12 Tf (\\310) Tj ET", "", false},
		{"image with SMask None", "/ImNone Do", "", false},
		{"image with SMaskInData", "/ImInd Do", "", true},
		{"shading pattern filled", "/Pattern cs /Sp scn 0 0 10 10 re f", "", false},
		{"uncoloured pattern filling at ca", "/Cs0 cs 0.5 /P2 scn 0 0 10 10 re f", "", true},
		{"soft mask before sh", "/sm gs /Sh sh", "", true},
		{"ca before an image", "/ca gs /Im Do", "", true},
		{"glyph past d1 filling at ca", "BT /T3 12 Tf (C) Tj ET", "", true},
		{"glyph filling at ca shown invisibly", "BT /T3 12 Tf 3 Tr (A) Tj ET", "", false},
		{"glyph filling at ca shown stroked", "BT /T3 12 Tf 1 Tr (A) Tj ET", "", true},
		{"appearance setting ca and painting nothing", "", appearance("/ca gs", "/Resources << /ExtGState << /ca << /ca 0.5 >> >> >>"), false},
		{"appearance filling at ca", "", appearance("/ca gs 0 0 5 5 re f", "/Resources << /ExtGState << /ca << /ca 0.5 >> >> >>"), true},
		{"appearance with a transparency group", "", appearance("0 0 5 5 re f", "/Group << /S /Transparency >>"), true},
		{"opaque appearance", "", appearance("0 0 5 5 re f", ""), false},
		{"appearance using the page's state", "", appearance("/ca gs 0 0 5 5 re f", ""), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := paintProbe(t, tt.data, tt.annot); got != tt.want {
				t.Errorf("contains transparency = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCheckTransparencyParts2PlusPaintingOperator pins the verdict the walk
// feeds: a page that sets /ca 0.5 and undoes it before its only fill needs no
// blending colour space under ISO 19005-2, 6.2.10 t2 or ISO 19005-4, 6.2.9
// t2, and one that fills under it does.
func TestCheckTransparencyParts2PlusPaintingOperator(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tt := range []struct {
		data string
		want bool
	}{
		{"q /GS gs Q 0 0 10 10 re f", false},
		{"/GS gs 0 0 10 10 re f", true},
	} {
		doc, cat := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R /Resources << /ExtGState << /GS << /ca 0.5 >> >> >> >>",
			streamObj(tt.data),
		)
		for _, p := range []struct {
			profile string
			part    int
			rule    Rule
		}{
			{"PDF/A-2b", 2, Rule_19005_2_6_2_10_t02},
			{"PDF/A-4", 4, Rule_19005_4_6_2_9_t02},
		} {
			got := checkTransparencyParts2Plus(ctx, doc, cat, pageCtxsFor(ctx, doc, cat), p.profile, p.part)
			if hasRule(got, p.rule) != tt.want {
				t.Errorf("%s on %q: blending space rule fired = %v, want %v", p.profile, tt.data, !tt.want, tt.want)
			}
		}
	}
}

// TestPaintTransparencyWalksAFormOnce pins that the walk is linear in the
// operators of the streams a document draws: a form every page paints, whose
// names all resolve in its own resources, is walked once for the whole
// document, whatever resources each page has.
func TestPaintTransparencyWalksAFormOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const pageCount = 50
	objs := []string{"<< /Type /Catalog /Pages 2 0 R >>", ""}
	kids := make([]string, 0, pageCount)
	// Object 3 is the form, and each page and its content follow it.
	objs = append(objs, "<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << /ExtGState << /G << /LW 1 >> >> >>"+
		" /Length 20 >>\nstream\n/G gs 0 0 10 10 re f\nendstream")
	for i := range pageCount {
		pageNum := 4 + 2*i
		kids = append(kids, fmt.Sprintf("%d 0 R", pageNum))
		objs = append(objs,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents %d 0 R /Resources << /XObject << /F 3 0 R >> >> >>", pageNum+1),
			streamObj("/F Do /F Do"))
	}
	objs[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), pageCount)
	doc, cat := objsDocPDF(t, objs...)
	pages := pageCtxsFor(ctx, doc, cat)
	w := newPaintTransparency(&validationCtx{doc: doc, profile: "PDF/A-2b", part: 2, pages: pages})
	for i := range pages {
		if w.pageTransparent(ctx, &pages[i]) {
			t.Fatalf("page %d paints no transparency", i+1)
		}
	}
	if len(w.memo) != 1 {
		t.Errorf("the form was walked %d times, want once", len(w.memo))
	}
}
