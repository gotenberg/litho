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
	// DetectNumbering parses section numbering (1.1, A.1, IV, §2.3, Chapter 5)
	// to improve heading detection and override font-size clustering with
	// the numbering depth. Default true.
	DetectNumbering bool
	// StripNumberingFromTitle removes the numeric prefix from generated
	// bookmark titles (so "1.1 Overview" becomes "Overview"). Default false.
	StripNumberingFromTitle bool
	// Scorer scores heading candidates. When nil, the default heuristic
	// scorer is used. Replace this for custom or ML-based heading detection.
	Scorer HeadingScorer
	// ScoreThreshold is the minimum score for a candidate to be included
	// as a heading. Default 0.5.
	ScoreThreshold float64
}

// DefaultOptions returns the recommended default options.
func DefaultOptions() Options {
	return Options{
		MaxDepth:            3,
		MinFontSizeRatio:    1.15,
		SkipHeaderFooter:    true,
		MaxTitleLength:      200,
		PreferStructureTree: true,
		DetectNumbering:     true,
		Scorer:              HeuristicScorer{},
		ScoreThreshold:      0.5,
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
	if o.Scorer == nil {
		o.Scorer = d.Scorer
	}
	if o.ScoreThreshold == 0 {
		o.ScoreThreshold = d.ScoreThreshold
	}
}
