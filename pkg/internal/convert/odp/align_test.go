package odp

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestAbsoluteStartEnd checks that a paragraph aligned start or end reads
// left or right in a presentation LibreOffice wrote, and keeps its
// logical alignment under another producer (see [reader.applyPara]).
func TestAbsoluteStartEnd(t *testing.T) {
	for _, tc := range []struct {
		generator, align string
		want             css.TextAlign
	}{
		{"LibreOffice/7.4.2.3$Linux_X86_64 LibreOffice_project/abc", "start", css.AlignLeft},
		{"LibreOffice/7.4.2.3$Linux_X86_64 LibreOffice_project/abc", "end", css.AlignRight},
		{"", "start", css.AlignStart},
		{"OpenOffice/4.1.7$Win32 OpenOffice.org_project/417m1$Build-9800", "end", css.AlignEnd},
	} {
		meta := ""
		if tc.generator != "" {
			meta = `<meta:generator>` + tc.generator + `</meta:generator>`
		}
		s := firstShape(t, flat{
			meta: meta,
			auto: `<style:style style:name="P1" style:family="paragraph"><style:paragraph-properties fo:text-align="` + tc.align + `"/></style:style>`,
			body: `<draw:page draw:name="p" draw:master-page-name="Default"><draw:frame svg:x="1cm" svg:y="1cm" svg:width="6cm" svg:height="3cm"><draw:text-box><text:p text:style-name="P1">hello</text:p></draw:text-box></draw:frame></draw:page>`,
		})
		if got := s.Text.Paragraphs[0].Align; got != tc.want {
			t.Errorf("%q %s: align %v, want %v", tc.generator, tc.align, got, tc.want)
		}
	}
}
