package odp

import (
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// handoutMaster reads the document's handout master into doc (ODF 1.3,
// Part 3, 10.2.1 style:handout-master): the size and margins of the page
// layout it names (16.5), the background of its drawing-page style, and
// its shapes: the header, footer, date-time and page-number frames as its
// drawing-page style's display flags show them, with the declarations its
// presentation:use-header-name, use-footer-name and use-date-time-name
// name (19.428 to 19.430), and its shapes of no class. Its draw:page-thumbnail elements are
// left out: they place the slides of the count the file was saved with,
// and the builder lays out the count the caller asks for, as Impress does
// when its print dialog names one. A document without a handout master
// prints its handout pages on the notes page size without one.
//
// # Corpus evidence
//
//   - probe, odp: the pinned LibreOffice writes the handout master of a
//     deck that has none with six draw:page-thumbnail elements, the
//     layout AL0T26, the page layout of the notes master and the display
//     flags header, footer and date-time true, page-number false. Given a
//     page layout of four, it keeps the thumbnails the file holds rather
//     than lay out four. It keeps the display flags of a handout master
//     it reads and drops its presentation:use-header-name and the other
//     two, so whether Impress prints those declarations on a handout is
//     not measured.
func (r *reader) handoutMaster(doc *slides.Document) {
	hm := odf.Section(r.pkg.Styles, "master-styles").Child(odf.NSStyle, "handout-master")
	if hm == nil {
		return
	}
	if pl := r.styles.PageLayouts[hm.A(odf.NSStyle, "page-layout-name")]; pl != nil {
		w, okW := odf.Length(pl.Page.Get("fo:page-width"))
		h, okH := odf.Length(pl.Page.Get("fo:page-height"))
		if okW && okH && w > 0 && h > 0 {
			doc.HandoutWidth, doc.HandoutHeight = w, h
		}
		for _, m := range []struct {
			side css.Side
			name string
		}{{css.Top, "fo:margin-top"}, {css.Right, "fo:margin-right"}, {css.Bottom, "fo:margin-bottom"}, {css.Left, "fo:margin-left"}} {
			if v, ok := odf.Length(pl.Page.Get(m.name)); ok && v > 0 {
				doc.HandoutMargins[m.side] = v
			}
		}
	}
	dp := r.styles.Resolve(odf.ScopeStyles, odf.FamilyDrawingPage, hm.A(odf.NSDraw, "style-name"), odf.DrawingPageProps)
	doc.HandoutBackground = r.fill(dp, "none")
	pc := &pageCtx{scope: odf.ScopeStyles, master: true, handout: true}
	pc.decls(r, hm, dp)
	doc.HandoutShapes = r.shapeList(hm.Children, pc, 0)
}
