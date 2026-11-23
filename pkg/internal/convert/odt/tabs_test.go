package odt

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// TestConvert_TabsFromTheIndent counts a paragraph's tab stops from its
// indent once: the text after a tab to a left stop at 3 cm in a paragraph
// indented by 2 cm starts 5 cm from the page area's edge, as LibreOffice
// prints the same paragraph (probe through the pinned image).
func TestConvert_TabsFromTheIndent(t *testing.T) {
	auto := `<style:style style:name="M2" style:family="paragraph"><style:paragraph-properties fo:margin-left="2cm"><style:tab-stops><style:tab-stop style:position="3cm" style:type="left"/></style:tab-stops></style:paragraph-properties></style:style>`
	res := convertDoc(t, doc{contentAuto: auto, body: `<text:p text:style-name="M2">ab<text:tab/>X</text:p>`}.pack(t), office.Options{})
	found := false
	for _, r := range runs(res.Pages[0]) {
		if r.text == "X" {
			found = true
			if !near(r.x, 198.43, 0.5) {
				t.Errorf("text after the tab at %.2f, want 198.43", r.x)
			}
		}
	}
	if !found {
		t.Fatal("X not drawn")
	}
}
