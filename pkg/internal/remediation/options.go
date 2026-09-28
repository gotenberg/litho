package remediation

// Options configures remediation behavior.
type Options struct {
	// Lang is the BCP 47 language tag for the catalog /Lang entry.
	// Defaults to "en" if empty.
	Lang string
	// PreserveContent controls whether fixers may alter visible page content.
	// When true (default), only structural/metadata fixes are applied.
	// When false, aggressive fixers are enabled that may change rendering
	// (e.g., stripping unknown operators, clamping integers, generating
	// empty annotation appearances, re-encoding LZW streams).
	PreserveContent bool
	// FigureAlt is the alternate text written into a Figure structure element
	// that lacks the /Alt (or /ActualText) PDF/UA requires. A processor cannot
	// describe an image, and a generic placeholder satisfies the validator's
	// presence check (ISO 14289-1 §7.3, ISO 14289-2 §8.2.5.28.2) while masking
	// the missing description from assistive technology, so it is never
	// fabricated by default. When set, this string is used in either mode. When
	// empty, the figure is left for a human in the default safe mode, and in
	// aggressive mode (PreserveContent == false) a Figure enclosing no content
	// is removed ([fixEmptyFigure]) and a generic placeholder is inserted in the
	// others so the document passes the mechanical check.
	FigureAlt string
	// PinnedID, when 16 bytes long, seeds the trailer /ID minted by the
	// trailer-id fixer instead of crypto/rand. Reproducible-build callers
	// pin it so identical inputs yield identical output bytes; the random
	// /ID is litho's documented nondeterminism seam.
	PinnedID []byte
	// Disabled names the fixers the run skips: the content-tier fixers whose
	// repairs a writer rolled back in an earlier round of the same
	// remediation ([Report.ContentFixers]). The violations they match stay
	// in the residual.
	Disabled []string
	// Fonts supplies the programs of the fonts the document shows without
	// embedding them ([fixFontEmbed]). Nil leaves every such font unembedded.
	Fonts FontProvider
	// WholeFonts embeds every program Fonts supplies whole instead of a
	// subset of the glyphs the document shows, so a later edit can show
	// characters the document does not show yet.
	WholeFonts bool
	// fonts collects the fonts a run embedded and the ones it left
	// unembedded although Fonts was set, for [Report.FontRefusals] and
	// [Report.FontsEmbedded]. [remediate] sets it for its run.
	fonts *fontLedger
}

// fontLedger is what one run of [fixFontEmbed] did with the fonts it met.
type fontLedger struct {
	refused  []FontRefusal
	embedded []FontEmbed
}

// WithDisabled returns a copy of o whose run skips the fixers named in names
// ([Options.Disabled]).
func (o Options) WithDisabled(names []string) Options {
	o.Disabled = names
	return o
}
