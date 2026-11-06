package html

// paperArea returns the size the media features of the document see: the
// page area of the paper and margins o names, before any @page rule.
// Chromium's print evaluates the width, height, aspect-ratio and
// orientation media features against the page area Page.printToPDF's paper
// size and margins leave, whatever size and margins an @page rule then
// gives the pages, and resolves the viewport units against the page area
// of the first page (see prepareDoc). CSS Page 3, 3.1 makes the page area
// the initial containing block and leaves the media features' size to the
// user agent. A margin pair wider than the paper leaves no area, which
// makes every size feature false.
func paperArea(o Options) (float64, float64) {
	w, h := o.PageWidth, o.PageHeight
	if o.Landscape {
		w, h = h, w
	}
	return max(w-o.MarginLeft-o.MarginRight, 0), max(h-o.MarginTop-o.MarginBottom, 0)
}
