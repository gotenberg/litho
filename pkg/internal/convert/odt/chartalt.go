package odt

import (
	"cmp"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// objectAlt returns the description of the chart the draw:object obj
// embeds (ODF 1.3 Part 3, 10.4.6), whose replacement picture the frame
// draws, and the language that description is in: the sentence
// [chart.AltText] derives from the chart's model, in English, and "en"
// when the document's language is another ([chart.AltTextLang]). It
// returns "" for an object that is no chart or does not parse: its
// picture then stays without a description, as without the option. The
// chart is read for its description alone; the page draws the
// replacement picture either way, so the chart's warnings are not
// reported.
func (b *builder) objectAlt(obj *odf.Element) (string, string) {
	var c *chart.Chart
	var err error
	if inline := obj.Child(odf.NSOffice, "document"); inline != nil {
		if inline.A(odf.NSOffice, "mimetype") != odf.MimeChart {
			return "", ""
		}
		c, err = odf.ChartFromDocument(inline, inline)
	} else {
		dir := strings.TrimSuffix(strings.TrimPrefix(obj.A(odf.NSXLink, "href"), "./"), "/")
		if dir == "" || strings.Contains(dir, "://") || (b.pkg.MediaType(dir+"/") != odf.MimeChart && b.pkg.MediaType(dir) != odf.MimeChart) {
			return "", ""
		}
		c, err = odf.ParseChart(b.ctx, b.pkg, dir)
	}
	if err != nil || c == nil {
		// A chart that does not parse is described by nothing; its
		// replacement picture still draws.
		return "", ""
	}
	return chart.AltText(c), chart.AltTextLang(cmp.Or(b.paraText("Standard").langTag(), b.defaultLang))
}
