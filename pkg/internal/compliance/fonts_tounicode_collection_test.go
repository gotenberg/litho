package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestType0ToUnicodeMapsThroughTheCollectionUCS2 pins the collections a Type 0
// font without a /ToUnicode value reaches Unicode through, read as veraPDF
// 1.30.2 reads them (PDType0Font.toUnicode): under Identity-H the CIDFont's
// /CIDSystemInfo, under any other encoding the CMap's, and a collection
// counts when veraPDF bundles its Adobe-<Ordering>-UCS2 CMap. Adobe-KR, the
// successor of Adobe-Korea1, is one, so a font over it is not reported.
func TestType0ToUnicodeMapsThroughTheCollectionUCS2(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const krCMap = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap\n" +
		"1 begincodespacerange <0000> <FFFF> endcodespacerange\n" +
		"1 begincidrange <0000> <FFFF> 0 endcidrange\nendcmap end end\n"
	stream := func(registry, ordering string) string {
		return fmt.Sprintf("<< /Type /CMap /CMapName /LithoCMap /CIDSystemInfo << /Registry (%s) /Ordering (%s) /Supplement 9 >> /Length %d >>\nstream\n%s\nendstream",
			registry, ordering, len(krCMap), krCMap)
	}
	cases := []struct {
		name     string
		encoding string // a name, or empty for the embedded stream
		cmapInfo [2]string
		cidFont  string
		profile  string
		fires    bool
	}{
		{"Identity-H over Adobe-KR", "Identity-H", [2]string{}, "KR", "PDF/A-2u", false},
		{"Identity-H over Adobe-KR at PDF/UA-1", "Identity-H", [2]string{}, "KR", "PDF/UA-1", false},
		{"Identity-H over Adobe-KR at PDF/UA-2", "Identity-H", [2]string{}, "KR", "PDF/UA-2", false},
		{"Identity-H over Adobe-Korea1", "Identity-H", [2]string{}, "Korea1", "PDF/A-2u", false},
		{"Identity-H over Adobe-Identity", "Identity-H", [2]string{}, "Identity", "PDF/A-2u", true},
		{"an embedded Adobe-KR CMap", "", [2]string{"Adobe", "KR"}, "KR", "PDF/A-2u", false},
		{"an embedded Adobe-KR CMap at PDF/A-3a", "", [2]string{"Adobe", "KR"}, "KR", "PDF/A-3a", false},
		// veraPDF keys a CMap other than Identity by the CMap's collection.
		{"UniJIS-UCS2-H over Adobe-Identity", "UniJIS-UCS2-H", [2]string{}, "Identity", "PDF/A-2u", false},
		{"an embedded Adobe-Identity CMap over Adobe-Japan1", "", [2]string{"Adobe", "Identity"}, "Japan1", "PDF/A-2u", true},
		// A name no table gives a collection reaches none.
		{"UniAKR-UTF16-H over Adobe-KR", "UniAKR-UTF16-H", [2]string{}, "KR", "PDF/A-2u", true},
		// ISO 32000-2 moved UniKS-UTF16-H to Adobe-KR, which maps too.
		{"UniKS-UTF16-H over Adobe-Korea1 at PDF/UA-2", "UniKS-UTF16-H", [2]string{}, "Korea1", "PDF/UA-2", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			enc := "/" + tc.encoding
			objs := []string{"<< /Type /Catalog >>"}
			if tc.encoding == "" {
				enc = "5 0 R"
			}
			objs = append(objs,
				"<< /Type /Font /Subtype /Type0 /BaseFont /LithoCJK /Encoding "+enc+" /DescendantFonts [3 0 R] >>",
				"<< /Type /Font /Subtype /CIDFontType0 /BaseFont /LithoCJK"+
					" /CIDSystemInfo << /Registry (Adobe) /Ordering ("+tc.cidFont+") /Supplement 9 >>"+
					" /FontDescriptor 4 0 R >>",
				"<< /Type /FontDescriptor /FontName /LithoCJK /Flags 4 >>",
			)
			if tc.encoding == "" {
				objs = append(objs, stream(tc.cmapInfo[0], tc.cmapInfo[1]))
			}
			doc, _ := objsDocPDF(t, objs...)
			type0, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
			if err != nil {
				t.Fatalf("resolve Type 0 font: %v", err)
			}
			cidFont, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
			if err != nil {
				t.Fatalf("resolve CIDFont: %v", err)
			}
			usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{32: true}}
			got := checkType0ToUnicode(ctx, doc, type0, cidFont, "F1", 1, tc.profile, usage)
			if fired := len(got) != 0; fired != tc.fires {
				t.Errorf("fired = %v (%v), want %v", fired, got, tc.fires)
			}
		})
	}
}
