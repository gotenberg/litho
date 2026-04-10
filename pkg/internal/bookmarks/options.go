package bookmarks

// Options controls bookmark generation behavior.
type Options struct {
	// MaxDepth limits how many heading levels are included. Default 3.
	MaxDepth int
	// MinFontSizeRatio sets the minimum font size (relative to body text)
	// for an element to be considered a heading. Default 1.15.
	// A value of 0 means auto-detect only (use the histogram gap).
	MinFontSizeRatio float64
	// SkipHeaderFooter filters out text that appears as a running header or
	// footer on many pages. Default true.
	SkipHeaderFooter bool
	// MaxTitleLength truncates long headings. Default 200.
	MaxTitleLength int
	// PreferStructureTree uses the PDF's structure tree (tagged PDFs) when
	// available. Default true.
	PreferStructureTree bool
}

// DefaultOptions returns the recommended default options.
func DefaultOptions() Options {
	return Options{
		MaxDepth:            3,
		MinFontSizeRatio:    1.15,
		SkipHeaderFooter:    true,
		MaxTitleLength:      200,
		PreferStructureTree: true,
	}
}

// applyDefaults fills in zero-valued fields with the defaults.
func (o *Options) applyDefaults() {
	d := DefaultOptions()
	if o.MaxDepth == 0 {
		o.MaxDepth = d.MaxDepth
	}
	if o.MinFontSizeRatio == 0 {
		o.MinFontSizeRatio = d.MinFontSizeRatio
	}
	if o.MaxTitleLength == 0 {
		o.MaxTitleLength = d.MaxTitleLength
	}
}
