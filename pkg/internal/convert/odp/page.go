package odp

import (
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// pageCtx is the page a shape is read for: the document its automatic
// styles resolve in, whether it is a master page's shape shown on the
// slide, the slide's background, which an automatic text colour reads,
// and what the slide's header, footer and date-time fields show.
type pageCtx struct {
	scope  odf.Scope
	master bool
	// mp is the slide's master page, nil for none.
	mp         *odf.MasterPage
	background slides.Fill
	number     int
	// footer and header are the texts of the slide's footer and header
	// declarations; showFooter, showHeader, showDate and showNumber its
	// drawing-page style's display flags.
	footer, header                               string
	showFooter, showHeader, showDate, showNumber bool
	// date is the slide's date-time declaration, nil for none.
	date *odf.Element
	// handout marks the handout master, whose header shows.
	handout bool
}

// decls reads which declarations and master fields the page pg shows
// (ODF 1.3, Part 3, 19.428 to 19.430 presentation:use-footer-name,
// use-date-time-name and use-header-name; 20.233 to 20.236 the display
// flags of the drawing-page style dp). A flag the style leaves unset
// takes Impress's default: the header, the footer and the date shown,
// the page number not.
func (pc *pageCtx) decls(r *reader, pg *odf.Element, dp odf.Props) {
	pc.footer = r.footers[pg.A(odf.NSPresentation, "use-footer-name")]
	pc.header = r.headers[pg.A(odf.NSPresentation, "use-header-name")]
	pc.date = r.dates[pg.A(odf.NSPresentation, "use-date-time-name")]
	pc.showFooter = dp.Get("presentation:display-footer") != "false"
	pc.showHeader = dp.Get("presentation:display-header") != "false"
	pc.showDate = dp.Get("presentation:display-date-time") != "false"
	pc.showNumber = dp.Get("presentation:display-page-number") == "true"
}

// masterShows reports whether a master page's shape of the presentation
// class named class shows on a slide: the placeholders of the layout (title,
// outline, subtitle, text, graphic, object, chart, table, orgchart,
// page, notes, handout) are templates the slide's own shapes fill and
// never print; the header shows on notes and handouts alone, on the
// handout master as its flag says; the footer, date-time and page-number
// placeholders as the page's flags say; a shape of no class always (ODF
// 1.3, Part 3, 19.393).
func (pc *pageCtx) masterShows(class string) bool {
	switch class {
	case "":
		return true
	case "header":
		return pc.handout && pc.showHeader
	case "footer":
		return pc.showFooter
	case "date-time":
		return pc.showDate
	case "page-number":
		return pc.showNumber
	}
	return false
}
