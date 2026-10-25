package mutations

import (
	"testing"
)

// inlineObjects returns a one-page document whose page content is body, and
// whose page also paints a form XObject (object 7) whose content is form.
// pageExtra and catalogExtra are spliced into the page and catalog
// dictionaries.
func inlineObjects(body, form, pageExtra, catalogExtra string) map[int]string {
	objs := copiesObjects(catalogExtra)
	objs[3] = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
		"/Resources << /XObject << /Fm 7 0 R >> >> " + pageExtra + " >>"
	objs[4] = streamObject("", []byte(body+" /Fm Do"))
	objs[7] = streamObject("/Type /XObject /Subtype /Form /BBox [0 0 10 10]", []byte(form))
	return objs
}

// secretInline is an inline image whose six grey samples spell SECRET.
const secretInline = "q 60 0 0 10 100 100 cm BI /W 6 /H 1 /CS /G /BPC 8 ID SECRET EI Q"

// TestScanOutputForTokens_SkipsInlineImageSamples pins that the samples of an
// inline image, the bytes between ID and EI (ISO 32000-1, 8.9.7), are not
// compared, in a page's content and in a form XObject's: text extraction never
// reads them (9.10.2).
func TestScanOutputForTokens_SkipsInlineImageSamples(t *testing.T) {
	t.Parallel()
	cases := map[string]map[int]string{
		"page content": inlineObjects(secretInline, "", "", ""),
		"form xobject": inlineObjects("", secretInline, "", ""),
		"two images":   inlineObjects(secretInline+" "+secretInline, secretInline+"\n"+secretInline, "", ""),
		"line feeds":   inlineObjects("BI /W 6 /H 1 /CS /G /BPC 8 ID\nSECRET\nEI", "", "", ""),
	}
	for name, objs := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if res := scanFor(t, copiesDoc(t, objs, nil, ""), "SECRET"); !res.Clean() {
				t.Errorf("inline image samples were compared: %+v", res)
			}
		})
	}
}

// TestScanOutputForTokens_ReadsAroundInlineImages pins what the skip keeps: a
// copy a text operator draws next to an inline image, literal or hexadecimal,
// in the page's content or a form's, and a copy in the image's dictionary,
// which is no sample.
func TestScanOutputForTokens_ReadsAroundInlineImages(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		objs   map[int]string
		objNum int
	}{
		"literal before": {inlineObjects("BT /F1 12 Tf (SECRET) Tj ET "+secretInline, "", "", ""), 4},
		"literal after":  {inlineObjects(secretInline+" BT /F1 12 Tf (SECRET) Tj ET", "", "", ""), 4},
		"hex after":      {inlineObjects(secretInline+" BT /F1 12 Tf <534543524554> Tj ET", "", "", ""), 4},
		"between images": {inlineObjects(secretInline+" BT (SECRET) Tj ET "+secretInline, "", "", ""), 4},
		"in a form":      {inlineObjects("", secretInline+" BT (SECRET) Tj ET", "", ""), 7},
		"in the dictionary": {inlineObjects(
			"BI /W 6 /H 1 /CS /G /BPC 8 /SECRET 1 ID \x00\x01\x02\x03\x04\x05 EI", "", "", ""), 4},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if res := scanFor(t, copiesDoc(t, tc.objs, nil, ""), "SECRET"); !hasHit(res, tc.objNum, "stream") {
				t.Errorf("the copy next to an inline image was not found: %+v", res)
			}
		})
	}
}

// TestScanOutputForTokens_ReadsBytesOneReaderTakesForOperators pins that the
// skip ends where the earlier of veraPDF and PDF.js ends the data. Here PDF.js
// ends it at the first EI and reads SECRET as an operator after it, while
// veraPDF reads on to the second EI, an undefined operator making the first
// part of the data, so SECRET is compared.
func TestScanOutputForTokens_ReadsBytesOneReaderTakesForOperators(t *testing.T) {
	t.Parallel()
	body := "BI /W 40 /H 1 /CS /G /BPC 8 ID \x00\x01 EI SECRET EI Q"
	if res := scanFor(t, copiesDoc(t, inlineObjects(body, "", "", ""), nil, ""), "SECRET"); !hasHit(res, 4, "stream") {
		t.Errorf("bytes PDF.js reads as an operator were skipped: %+v", res)
	}
}

// TestScanOutputForTokens_ReadsInlineSamplesAlsoReadAsBytes pins that a
// content stream a slot that reads a stream as bytes also names, an embedded
// file here, is compared whole, whichever reference the walk meets first. The
// walk ranges dictionaries, so repeating the scan meets both orders.
func TestScanOutputForTokens_ReadsInlineSamplesAlsoReadAsBytes(t *testing.T) {
	t.Parallel()
	doc := copiesDoc(t, inlineObjects("", secretInline, "",
		"/AF [<< /Type /Filespec /F (a.txt) /UF (a.txt) /EF << /F 7 0 R >> >>]"), nil, "")
	for range 20 {
		if res := scanFor(t, doc, "SECRET"); !hasHit(res, 7, "stream") {
			t.Fatalf("a form also read as bytes was compared without its samples: %+v", res)
		}
	}
}

// type3Objects returns a one-page document whose page shows a glyph of a Type 3
// font (object 8) whose description, object 9, is glyph. procsRef writes the
// /CharProcs dictionary as an object of its own (object 10), and fontType is
// the font's /Type.
func type3Objects(glyph string, procsRef bool, fontType string) map[int]string {
	objs := copiesObjects("")
	objs[3] = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
		"/Resources << /Font << /T3 8 0 R >> >> >>"
	objs[4] = streamObject("", []byte("BT /T3 12 Tf 100 600 Td (a) Tj ET"))
	procs := "<< /a 9 0 R >>"
	if procsRef {
		objs[10] = procs
		procs = "10 0 R"
	}
	objs[8] = "<< /Type /" + fontType + " /Subtype /Type3 /FontBBox [0 0 1000 1000] " +
		"/FontMatrix [0.001 0 0 0.001 0 0] /CharProcs " + procs + " /Encoding << /Differences [97 /a] >> " +
		"/FirstChar 97 /LastChar 97 /Widths [1000] /Resources << /Font << /F1 << /Type /Font " +
		"/Subtype /Type1 /BaseFont /Helvetica >> >> >> >>"
	objs[9] = streamObject("", []byte("1000 0 d0 "+glyph))
	return objs
}

// TestScanOutputForTokens_SkipsInlineImageSamplesInGlyphs pins that a Type 3
// glyph description, a content stream reached through the font's /CharProcs
// (ISO 32000-1, 9.6.5) whose dictionary carries no /Subtype, has its inline
// image samples skipped as a page's and a form's are.
func TestScanOutputForTokens_SkipsInlineImageSamplesInGlyphs(t *testing.T) {
	t.Parallel()
	cases := map[string]map[int]string{
		"direct char procs":   type3Objects(secretInline, false, "Font"),
		"indirect char procs": type3Objects(secretInline, true, "Font"),
	}
	for name, objs := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := copiesDoc(t, objs, nil, "")
			// The walk ranges dictionaries, so repeating the scan meets the
			// glyph before and after the font's other entries.
			for range 20 {
				if res := scanFor(t, doc, "SECRET"); !res.Clean() {
					t.Fatalf("inline image samples in a glyph were compared: %+v", res)
				}
			}
		})
	}
}

// TestScanOutputForTokens_ReadsTextInGlyphs pins what the glyph reading keeps:
// a copy a glyph description draws as text, literal or hexadecimal, next to an
// inline image, and the samples of a glyph whose font names itself something
// other than a font, which is not trusted to be one.
func TestScanOutputForTokens_ReadsTextInGlyphs(t *testing.T) {
	t.Parallel()
	cases := map[string]map[int]string{
		"literal":       type3Objects(secretInline+" BT /F1 12 Tf (SECRET) Tj ET", false, "Font"),
		"hexadecimal":   type3Objects(secretInline+" BT /F1 12 Tf <534543524554> Tj ET", true, "Font"),
		"not a font":    type3Objects(secretInline, false, "XObject"),
		"text no image": type3Objects("BT /F1 12 Tf (SECRET) Tj ET", false, "Font"),
	}
	for name, objs := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if res := scanFor(t, copiesDoc(t, objs, nil, ""), "SECRET"); !hasHit(res, 9, "stream") {
				t.Errorf("the copy in the glyph description was not found: %+v", res)
			}
		})
	}
}

// TestScanOutputForTokens_ReadsGlyphSamplesAlsoReadAsBytes pins that a glyph
// description a slot that reads a stream as bytes also names is compared
// whole, whichever reference the walk meets first.
func TestScanOutputForTokens_ReadsGlyphSamplesAlsoReadAsBytes(t *testing.T) {
	t.Parallel()
	objs := type3Objects(secretInline, false, "Font")
	objs[1] = "<< /Type /Catalog /Pages 2 0 R " +
		"/AF [<< /Type /Filespec /F (a.txt) /UF (a.txt) /EF << /F 9 0 R >> >>] >>"
	doc := copiesDoc(t, objs, nil, "")
	for range 20 {
		if res := scanFor(t, doc, "SECRET"); !hasHit(res, 9, "stream") {
			t.Fatalf("a glyph also read as bytes was compared without its samples: %+v", res)
		}
	}
}
