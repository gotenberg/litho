package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestXMPCheckDocInfoSync_NonStringInfoEntry pins how 6.7.3 compares an /Info
// entry that holds no string with its XMP analogue, as veraPDF 1.30 does,
// measured on crafted PDF/A-1b files: an integer, a real and a name compare as
// the text Java renders for them; a stream, a dictionary, an array and a
// boolean match no analogue, an empty one included; and a null value or a
// reference to an undefined object passes as an absent entry does.
//
// litho read every non-string value as an empty /Title, so an empty dc:title
// passed where veraPDF fails, which is how the remediated output of a file
// whose /Title is a font program came out failing veraPDF alone.
func TestXMPCheckDocInfoSync_NonStringInfoEntry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	packet := func(title string) []byte {
		return xmpPacket(`<rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">` +
			`<dc:title><rdf:Alt><rdf:li xml:lang="x-default">` + title + `</rdf:li></rdf:Alt></dc:title></rdf:Description>`)
	}

	for _, tc := range []struct {
		name, value, dcTitle string
		wantFire             bool
	}{
		{name: "stream against an empty dc:title", value: "3 0 R", dcTitle: "", wantFire: true},
		{name: "stream against its text", value: "3 0 R", dcTitle: "Test title", wantFire: true},
		{name: "dictionary against an empty dc:title", value: "<< /A 1 >>", dcTitle: "", wantFire: true},
		{name: "array against its element", value: "[(T)]", dcTitle: "T", wantFire: true},
		{name: "boolean against its spelling", value: "true", dcTitle: "true", wantFire: true},
		{name: "integer against its digits", value: "5", dcTitle: "5", wantFire: false},
		{name: "integer against an empty dc:title", value: "5", dcTitle: "", wantFire: true},
		{name: "real against its Java spelling", value: ".5", dcTitle: "0.5", wantFire: false},
		{name: "integral real against its Java spelling", value: "5.0", dcTitle: "5.0", wantFire: false},
		{name: "real against its PDF spelling", value: ".5", dcTitle: ".5", wantFire: true},
		{name: "name against itself", value: "/T", dcTitle: "/T", wantFire: false},
		{name: "escaped name against its escape", value: "/A#20B", dcTitle: "/A#20B", wantFire: false},
		{name: "name against its bare text", value: "/T", dcTitle: "T", wantFire: true},
		{name: "null", value: "null", dcTitle: "Anything", wantFire: false},
		{name: "reference to an undefined object", value: "99 0 R", dcTitle: "Anything", wantFire: false},
		{name: "indirect string", value: "4 0 R", dcTitle: "T", wantFire: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog >>",
				"<< /Title "+tc.value+" >>",
				"<< /Length 10 >>\nstream\nTest title\nendstream",
				"(T)",
			)
			doc.Index.Trailer["Info"] = ast.PdfIndirectRef{ObjectNum: 2}

			var results []Violation
			xmpCheckDocInfoSync(ctx, doc, packet(tc.dcTitle), 1, &results)

			fired := false
			for _, v := range results {
				if v.Rule == Rule_19005_1_6_7_3_t02 {
					fired = true
				}
			}
			if fired != tc.wantFire {
				t.Errorf("6.7.3 t2 fired = %v, want %v (%v)", fired, tc.wantFire, results)
			}
		})
	}
}

// TestXMPCheckDocInfoSync_NonStringDateEntry pins the date tests apart: 6.7.3
// tests 1 and 8 read "doCreationDatesMatch != false", which veraPDF 1.30
// leaves null for a /CreationDate or /ModDate that holds no string, so every
// such entry passes, against an absent, empty or dated analogue alike. Read
// as a text entry, a boolean date matched no analogue and failed both tests.
func TestXMPCheckDocInfoSync_NonStringDateEntry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, value := range []string{"true", "5", "/T", "<< /A 1 >>", "3 0 R"} {
		for _, analogue := range []string{"", "<xmp:CreateDate/><xmp:ModifyDate/>", "<xmp:CreateDate>2020-01-01T00:00:00Z</xmp:CreateDate>"} {
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog >>",
				"<< /CreationDate "+value+" /ModDate "+value+" >>",
				"<< /Length 10 >>\nstream\nTest title\nendstream",
			)
			doc.Index.Trailer["Info"] = ast.PdfIndirectRef{ObjectNum: 2}
			packet := xmpPacket(`<rdf:Description rdf:about="" xmlns:xmp="http://ns.adobe.com/xap/1.0/">` + analogue + `</rdf:Description>`)

			var results []Violation
			xmpCheckDocInfoSync(ctx, doc, packet, 1, &results)
			for _, v := range results {
				t.Errorf("date %s against %q: %v", value, analogue, v)
			}
		}
	}
}

// TestJavaDoubleString pins the spelling veraPDF gives a real, which is Java's
// Double.toString.
func TestJavaDoubleString(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in   float64
		want string
	}{
		{5, "5.0"},
		{5.5, "5.5"},
		{0.5, "0.5"},
		{-3.25, "-3.25"},
		{0, "0.0"},
		{1e7, "1.0E7"},
		{1.5e-4, "1.5E-4"},
	} {
		if got := javaDoubleString(tc.in); got != tc.want {
			t.Errorf("javaDoubleString(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
