package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// syncRulesFired runs the 6.7.3 comparison of an /Info dictionary against a
// packet whose rdf:Description holds body, and returns the tests that fired.
func syncRulesFired(t *testing.T, info, body string) map[Rule]bool {
	t.Helper()
	doc, _ := objsDocPDF(t, "<< /Type /Catalog >>", info)
	doc.Index.Trailer["Info"] = ast.PdfIndirectRef{ObjectNum: 2}
	packet := xmpPacket(`<rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/"` +
		` xmlns:pdf="http://ns.adobe.com/pdf/1.3/">` + body + `</rdf:Description>`)
	var results []Violation
	xmpCheckDocInfoSync(context.Background(), doc, packet, 1, &results)
	fired := make(map[Rule]bool)
	for _, v := range results {
		fired[v.Rule] = true
	}
	return fired
}

// TestXMPCheckDocInfoSync_EmptyContainers pins how 6.7.3 reads a dc:title,
// dc:creator or dc:description whose container holds no item, or an empty one,
// measured on veraPDF 1.30.2 with crafted PDF/A-1b files.
//
// XMPTitle, XMPCreator and XMPDescription are the first item of the container
// (the x-default one of an rdf:Alt when it has one), and null when there is
// none: an empty /Title against an empty rdf:Alt fails test 2, and against an
// rdf:Alt holding one empty item, whatever its language, passes. Test 3 also
// requires XMPCreatorSize == 1, which counts every item, an empty one
// included.
func TestXMPCheckDocInfoSync_EmptyContainers(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, info, body string
		rule             Rule
		wantFire         bool
	}{
		{"empty /Title, empty rdf:Alt", "<< /Title () >>", "<dc:title><rdf:Alt/></dc:title>", Rule_19005_1_6_7_3_t02, true},
		{"empty /Title, rdf:Alt of whitespace", "<< /Title () >>", "<dc:title><rdf:Alt>\n </rdf:Alt></dc:title>", Rule_19005_1_6_7_3_t02, true},
		{"empty /Title, empty x-default item", "<< /Title () >>", `<dc:title><rdf:Alt><rdf:li xml:lang="x-default"/></rdf:Alt></dc:title>`, Rule_19005_1_6_7_3_t02, false},
		{"empty /Title, empty fr item", "<< /Title () >>", `<dc:title><rdf:Alt><rdf:li xml:lang="fr"/></rdf:Alt></dc:title>`, Rule_19005_1_6_7_3_t02, false},
		{"empty /Subject, empty rdf:Alt", "<< /Subject () >>", "<dc:description><rdf:Alt/></dc:description>", Rule_19005_1_6_7_3_t04, true},
		{"empty /Subject, empty item", "<< /Subject () >>", `<dc:description><rdf:Alt><rdf:li xml:lang="x-default"></rdf:li></rdf:Alt></dc:description>`, Rule_19005_1_6_7_3_t04, false},
		{"empty /Author, empty rdf:Seq", "<< /Author () >>", "<dc:creator><rdf:Seq/></dc:creator>", Rule_19005_1_6_7_3_t03, true},
		{"empty /Author, empty rdf:Alt", "<< /Author () >>", "<dc:creator><rdf:Alt/></dc:creator>", Rule_19005_1_6_7_3_t03, true},
		{"empty /Author, one empty item", "<< /Author () >>", "<dc:creator><rdf:Seq><rdf:li/></rdf:Seq></dc:creator>", Rule_19005_1_6_7_3_t03, false},
		{"empty /Author, an empty item then x", "<< /Author () >>", "<dc:creator><rdf:Seq><rdf:li/><rdf:li>x</rdf:li></rdf:Seq></dc:creator>", Rule_19005_1_6_7_3_t03, true},
		{"/Author x, x then an empty item", "<< /Author (x) >>", "<dc:creator><rdf:Seq><rdf:li>x</rdf:li><rdf:li/></rdf:Seq></dc:creator>", Rule_19005_1_6_7_3_t03, true},
		{"/Author x, one item x", "<< /Author (x) >>", "<dc:creator><rdf:Seq><rdf:li>x</rdf:li></rdf:Seq></dc:creator>", Rule_19005_1_6_7_3_t03, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := syncRulesFired(t, tc.info, tc.body)[tc.rule]; got != tc.wantFire {
				t.Errorf("%s fired = %v, want %v", tc.rule.Clause, got, tc.wantFire)
			}
		})
	}
}

// TestXMPCheckDocInfoSync_ControlCharacters pins 6.7.3 against an /Info text
// entry holding a control character, measured on veraPDF 1.30.2 with crafted
// PDF/A-1b files. veraPDF reads a byte PDFDocEncoding leaves undefined as the
// control character it is, and a packet cannot carry one: XMPCore reads a raw
// control character or a reference to one as a space. So no analogue equals
// such an entry, whether the packet drops the character, spaces it or
// references it. A byte PDFDocEncoding maps to a spacing accent (0x18 to 0x1F)
// is that accent, and equals it.
func TestXMPCheckDocInfoSync_ControlCharacters(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, info, producer string
		wantFire             bool
	}{
		{"0x03 against the text without it", `<< /Producer (\003 version) >>`, " version", true},
		{"0x03 against a space", `<< /Producer (\003 version) >>`, "  version", true},
		{"0x03 against a reference to it", `<< /Producer (\003 version) >>`, "&#3; version", true},
		{"0x7F against the text without it", `<< /Producer (\177 version) >>`, " version", true},
		{"form feed against a space", `<< /Producer (\014 version) >>`, "  version", true},
		{"UTF-16 U+0003 against the text without it", `<< /Producer <FEFF000300200076> >>`, " v", true},
		{"only a control character, against an empty analogue", `<< /Producer (\003) >>`, "", true},
		{"0x18 against the breve it encodes", `<< /Producer (\030 version) >>`, "˘ version", false},
		{"spaces against spaces", `<< /Producer (  version) >>`, "  version", false},
		{"one trailing NUL against the text without it", `<< /Producer (ab\000) >>`, "ab", false},
		{"UTF-16 trailing NUL against the text without it", `<< /Producer <FEFF006100620000> >>`, "ab", false},
		{"only a NUL, against an empty analogue", `<< /Producer (\000) >>`, "", false},
		{"two trailing NULs against the text without them", `<< /Producer (ab\000\000) >>`, "ab", true},
		{"a middle NUL against the text without it", `<< /Producer (a\000b) >>`, "ab", true},
		{"a leading NUL against the text without it", `<< /Producer (\000ab) >>`, "ab", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := "<pdf:Producer>" + tc.producer + "</pdf:Producer>"
			if got := syncRulesFired(t, tc.info, body)[Rule_19005_1_6_7_3_t07]; got != tc.wantFire {
				t.Errorf("6.7.3 t7 fired = %v, want %v", got, tc.wantFire)
			}
		})
	}
}
