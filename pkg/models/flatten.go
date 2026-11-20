package models

// FlattenedPage describes what
// [github.com/gotenberg/litho/pkg/pdf.Writer.FlattenTransparency] did to one
// page. Concurrent reads of a value are safe; a write needs synchronization.
type FlattenedPage struct {
	// Page is the 1-based page number.
	Page int
	// Regions counts the areas composited into opaque images, and DPI the
	// resolution they were composited at, 0 when there were none.
	Regions int
	DPI     float64
	// CoarseMasks counts the soft masks composited at a fraction of DPI: their
	// groups did not fit
	// [Limits.MaxRenderPixels] even at the
	// lowest resolution flattening tries, so their opacity is approximated
	// within a few pixels of where each mask changes. Flattening refuses a
	// page whose transparency does not fit otherwise.
	CoarseMasks int
	// Rasterized counts the painting operations (paths, images, shadings,
	// form XObjects) whose painting the images took over.
	Rasterized int
	// KeptText counts the text objects, and forms showing text, inside a
	// region that stay text, drawn above its image.
	KeptText int
	// TextUnderTransparency counts the text objects, and forms showing
	// text, that transparent content drawn over them left visible: text
	// stays above the image, so they draw without that content's tint over
	// them. Their look is approximated. Text the source hid under content
	// drawn after it stays hidden and is not counted.
	TextUnderTransparency int
	// TransparentText counts the text objects whose own transparency the
	// output approximates: a constant alpha composited over white where the
	// text lay over other content, a soft mask or a blend mode dropped. Text
	// on the paper and text under an alpha of 0 are drawn exactly and not
	// counted.
	TransparentText int
	// GroupRemoved reports that the page's transparency group was removed.
	GroupRemoved bool
	// GroupSpaceConverted reports that the page's transparency group blended
	// in the grey or CMYK space its /CS names, which litho's renderer reads a
	// page painting with transparency in, the composites included. What the
	// composites leave of the page, and its annotations' appearances, were
	// converted to that space, so the page reads as it did once the group is
	// gone (see
	// [github.com/gotenberg/litho/pkg/pdf.Writer.FlattenTransparency]).
	GroupSpaceConverted bool
	// GroupSpaceUnconverted reports such a page whose content stayed in its own
	// colour spaces. The conversion would have rewritten an object another page
	// paints and does not convert to that space, or met content it cannot read
	// whole, or a group space it cannot write. Its colours outside the composites
	// read in the device's space, as viewers that blend every page in RGB, such
	// as poppler, read the source.
	GroupSpaceUnconverted bool
	// Annotations counts the annotations whose appearance was drawn into
	// the page's content, and EmptiedAnnotations those painting with
	// transparency that a viewer does not display, whose appearance was
	// removed with nothing drawn in its place.
	Annotations        int
	EmptiedAnnotations int
	// JPEGImages counts the composites, or tiles of a composite, encoded as
	// JPEG ([github.com/gotenberg/litho/pkg/pdf.WithFlattenJPEGQuality]).
	JPEGImages int
	// Overprinted counts the painting operations under overprint (ISO 32000-1,
	// 8.6.7) whose painting inside a region a composite took over. An image
	// paints every ink it holds, so on a device that overprints their
	// overprinting is lost, unless
	// [github.com/gotenberg/litho/pkg/pdf.WithFlattenOverprintSimulation]
	// composites it into the image. Viewers draw without overprinting by
	// default, so the loss shows only in print or in an overprint preview.
	Overprinted int
}

// FlattenReport describes what
// [github.com/gotenberg/litho/pkg/pdf.Writer.FlattenTransparency] did.
// Concurrent reads of a value are safe; a write needs synchronization.
type FlattenReport struct {
	// Pages lists the pages it changed, in order.
	Pages []FlattenedPage
	// TransparentAnnotations counts the annotations left painting with
	// transparency, which
	// [github.com/gotenberg/litho/pkg/pdf.Writer.FlattenTransparency] does not
	// draw into the page: form field widgets, which
	// [github.com/gotenberg/litho/pkg/pdf.Writer.FlattenForm] bakes, pop-ups,
	// annotations under optional content or kept upright on a rotated page,
	// and annotations with no appearance litho can draw.
	TransparentAnnotations int
	// SkippedPages lists the 1-based numbers of the pages whose content does
	// not read, a stream that does not decode or a reference to an object
	// the file lacks: they are left as they are, their transparency
	// included, as a viewer draws them.
	SkippedPages []int
}
