package css

// This file defines the sizing keywords of width, height and their
// minimums and maximums (CSS Sizing 3, 3.1 and 3.2; CSS Sizing 4, 3.1):
// min-content, max-content, fit-content and stretch compute to the
// [MinContent], [MaxContent], [FitContent] and [Stretch] kinds of a
// [Dimension], which the layout resolves against the box's content and
// containing block. fit-content() with an argument (CSS Sizing 4, 3.2)
// is not a size Chromium accepts on width or height, so it stays invalid
// there; it sizes grid tracks alone (CSS Grid 1, 7.2.1).

// sizingKeywords are the sizing keywords and their kinds.
var sizingKeywords = map[string]DimKind{
	"min-content": MinContent, "-webkit-min-content": MinContent, "-moz-min-content": MinContent,
	"max-content": MaxContent, "-webkit-max-content": MaxContent, "-moz-max-content": MaxContent,
	"fit-content": FitContent, "-webkit-fit-content": FitContent, "-moz-fit-content": FitContent,
	"stretch": Stretch, "-webkit-fill-available": Stretch, "-moz-available": Stretch,
}

// withSizing returns kws with the sizing keywords added.
func withSizing(kws map[string]DimKind) map[string]DimKind {
	for k, v := range sizingKeywords {
		kws[k] = v
	}
	return kws
}

// IsSizingKeyword reports a dimension that holds a sizing keyword:
// min-content, max-content, fit-content or stretch.
func (d Dimension) IsSizingKeyword() bool {
	return d.Kind == MinContent || d.Kind == MaxContent || d.Kind == FitContent || d.Kind == Stretch
}
