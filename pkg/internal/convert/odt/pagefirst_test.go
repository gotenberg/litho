package odt

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// TestConvert_FirstFooterOfEachMaster prints the first footer of a master
// page on the page that style:next-style-name moves to it, the first of
// its row, as LibreOffice prints alternating master pages.
func TestConvert_FirstFooterOfEachMaster(t *testing.T) {
	masters := `<style:master-page style:name="Standard" style:page-layout-name="pm1" style:next-style-name="B"><style:footer><text:p>A rest</text:p></style:footer><loext:footer-first><text:p>A first</text:p></loext:footer-first></style:master-page>` +
		`<style:master-page style:name="B" style:page-layout-name="pm1" style:next-style-name="Standard"><style:footer><text:p>B rest</text:p></style:footer><loext:footer-first><text:p>B first</text:p></loext:footer-first></style:master-page>`
	auto := `<style:page-layout style:name="pm1"><style:page-layout-properties fo:page-width="21cm" fo:page-height="29.7cm" fo:margin="2cm"/><style:footer-style><style:header-footer-properties fo:min-height="1cm"/></style:footer-style></style:page-layout>`
	contentAuto := `<style:style style:name="Brk" style:family="paragraph"><style:paragraph-properties fo:break-before="page"/></style:style>`
	body := `<text:p>one</text:p><text:p text:style-name="Brk">two</text:p><text:p text:style-name="Brk">three</text:p>`
	res := convertDoc(t, doc{autoStyles: auto, masters: masters, contentAuto: contentAuto, body: body}.pack(t), office.Options{})
	want := []string{"A first", "B first", "A first"}
	if len(res.Pages) != len(want) {
		t.Fatalf("%d pages, want %d", len(res.Pages), len(want))
	}
	for i, w := range want {
		if text := pageText(res.Pages[i]); !strings.Contains(text, w) {
			t.Errorf("page %d text %q lacks %q", i+1, text, w)
		}
	}
}
