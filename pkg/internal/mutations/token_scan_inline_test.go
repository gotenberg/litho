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
