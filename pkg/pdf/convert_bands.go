package pdf

import (
	htmlconv "github.com/gotenberg/litho/pkg/internal/convert/html"
	"github.com/gotenberg/litho/pkg/models"
)

// configBand is a band of [WithHeader] or [WithFooter].
type configBand struct {
	band   models.PageBand
	footer bool
}

// htmlBands returns the bands of c as the HTML converter takes them.
func (c *convertConfig) htmlBands() []htmlconv.Band {
	if len(c.bands) == 0 {
		return nil
	}
	out := make([]htmlconv.Band, len(c.bands))
	for i, b := range c.bands {
		room := htmlconv.RoomMargin
		if b.band.GrowMargin {
			room = htmlconv.RoomContent
		}
		out[i] = htmlconv.Band{
			Footer:   b.footer,
			HTML:     b.band.HTML,
			CSS:      b.band.CSS,
			Pages:    b.band.Pages,
			Room:     room,
			Isolated: b.band.IgnoreDocumentStyles,
		}
	}
	return out
}

// pageCounterStart returns the page counter's value before the first page
// [WithFirstPageNumber] asks for.
func (c *convertConfig) pageCounterStart() int {
	if !c.firstPageNumberSet {
		return 0
	}
	if c.firstPageNumber < htmlconv.MinPageCounterStart+1 {
		// Below the range: kept below it, refused by the converter.
		return htmlconv.MinPageCounterStart - 1
	}
	return c.firstPageNumber - 1
}
