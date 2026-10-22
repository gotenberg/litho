package pdf

import "github.com/gotenberg/litho/pkg/internal/limits"

// Limits bounds the resources litho spends on one document: how many bytes a
// stream may decode to, how many pixels an image may hold, how large an
// embedded PDF validation opens, how deeply form XObjects nest, how many
// parsed objects stay cached, and how many pixels a rendered page may hold. Each budget is an anti-DoS guard, not a
// requirement of ISO 32000: a few kilobytes of crafted input can otherwise ask
// for gigabytes of memory or unbounded work. The defaults ([DefaultLimits])
// sit far above what real documents need.
//
// A zero field keeps its default, so Limits{MaxStreamBytes: 1 << 30} raises
// one budget and leaves the others alone. A negative field, or one above its
// ceiling, makes [Open] fail with [ErrInvalidLimits], and [Limits.Validate]
// reports the same error without opening a document. Pass it to [Open] or
// [OpenFile] with [WithLimits].
//
// The recursion guards that protect litho's own stack are not configurable:
// the depth of the page tree, of nested arrays and dictionaries, of name,
// number and structure trees, of CMap chains, and the like. A Go stack
// overflow is a fatal error that no recover can catch, so those bounds stay
// fixed at values real documents never approach.
//
// Limits is a plain value, safe to copy and share. [Open] reads it once, and
// the [Reader] applies it for its whole life, to every document a [Writer] or
// [Pipeline] re-reads from it too.
type Limits struct {
	// MaxStreamBytes bounds the decoded bytes of one stream: the output of
	// each FlateDecode, LZWDecode, BrotliDecode, RunLengthDecode and
	// CCITTFaxDecode filter in its chain, of a filter plugin, and of a
	// linearization hint stream. A stream past it fails to decode with
	// [ErrStreamTooLarge]: [Reader.DecodeImage], an image's Decode function
	// and the Open function of an image or an embedded file return that
	// error, and a stream litho reads for its own use, such as a content
	// stream, is skipped as a damaged one would be. The image codecs,
	// DCTDecode, JPXDecode and JBIG2Decode, are bounded by MaxImagePixels
	// instead. An Open function streams a single FlateDecode, LZWDecode or
	// BrotliDecode filter without holding its output, and that read is not
	// bounded. Default 256 MiB, at most 4 GiB.
	//
	// Four further bounds move with it. The content of one page, summed
	// across the streams of its /Contents array, may take a quarter of it, 64
	// MiB by default: text extraction, validation, remediation, auto-tagging,
	// stamping and redaction refuse a page with more, as they refuse a damaged
	// one, and a call that fails on it returns [ErrPageContentTooLarge]. The
	// redaction scan reads no more than that from any one stream. A quarter
	// and not the whole, because the operators parsed from a page hold about
	// five times its bytes. The decoded object streams the
	// [Reader] keeps cached may take the whole of it, and the decoded font
	// programs and the colour transforms built from ICC profiles it keeps
	// cached a quarter each, 64 MiB by default: when one would take more, its
	// cache is emptied and refilled, which costs re-decoding, never a wrong
	// answer.
	MaxStreamBytes int64

	// MaxImagePixels bounds Width x Height of one image. [Reader.DecodeImage]
	// and an image's Decode function refuse a larger image before decoding
	// it, and decoded samples past four bytes a pixel of it, with
	// [ErrImageTooLarge]. The sample caps of the DCTDecode and JPXDecode
	// codecs scale with it, so a codestream is held to the same budget
	// whatever its dictionary declares. JBIG2Decode, one bit a pixel, is held
	// to its bytes: a bitmap may take a byte for each pixel of the budget, so
	// a mask past it, which is read reduced to the size it is drawn at, still
	// decodes. Default 100,000,000, at most 2^30.
	MaxImagePixels int64

	// MaxEmbeddedFileBytes bounds the decoded size of an embedded PDF that
	// PDF/A validation opens to check its conformance (ISO 19005-2, 6.8;
	// ISO 19005-4, 6.9). A larger embedded PDF is not validated and raises no
	// violation. An embedded file that is not a PDF is judged from its header
	// and reported whatever its size. Default 50 MiB, at most 4 GiB.
	MaxEmbeddedFileBytes int64

	// MaxFormDepth bounds how many form XObjects nest below the page: a form
	// the page paints directly, or an annotation's appearance, is at depth 1,
	// and a form that form paints at depth 2. Text extraction, image listing,
	// compliance validation and auto-tagging read forms down to depth
	// MaxFormDepth, and skip deeper ones without an error: their text is not
	// extracted, their images are not listed and their content is not
	// checked. Compliance counts a tiling pattern and a Type 3 glyph
	// description as one level each, as a form. Forms that share sub-forms
	// cost work that grows with the depth, so raise it only as far as the
	// documents need. Default 10, at most 256.
	MaxFormDepth int

	// MaxCachedObjects bounds how many parsed objects the [Reader] keeps.
	// When the cache is full it is emptied and refilled, which costs
	// re-parsing, never a wrong answer. Default 50,000, at most 8,388,607, the
	// most indirect objects ISO 32000-1, Annex C allows a file.
	MaxCachedObjects int

	// MaxRenderPixels bounds Width x Height of one page [Reader.RenderPage]
	// draws, and of every raster it allocates at the page's size, such as a
	// clip. A page whose size at the requested resolution exceeds it fails
	// with [ErrRenderTooLarge] before anything is drawn. The renderer holds
	// 16 bytes a pixel for the page. Default 50,000,000, a US Letter or A4
	// page at 600 dpi, at most 2^30.
	MaxRenderPixels int64
}

// DefaultLimits returns the budgets a [Reader] applies when [WithLimits] is
// not given, every field filled in.
func DefaultLimits() Limits {
	return limitsFromInternal(limits.Default())
}

// WithLimits sets the resource budgets the document is read under. A zero
// field of l keeps its default; [Open] fails with the error [Limits.Validate]
// returns, wrapping [ErrInvalidLimits], when a field is negative or above its
// ceiling. See [Limits].
func WithLimits(l Limits) OpenOption {
	return func(o *openOpts) {
		o.limits = l
	}
}

// Validate reports the first budget of l that is negative or above its
// ceiling, as an error wrapping [ErrInvalidLimits] that names the field, its
// value and the bound it breaks. A zero budget is valid: it stands for its
// default. [Open] and [OpenFile] return this same error for the budgets
// [WithLimits] gives them, so a caller that takes budgets from configuration
// or flags can refuse a bad one before opening any document.
func (l Limits) Validate() error {
	return l.internal().Validate()
}

// internal translates l to the budgets the internal packages read.
func (l Limits) internal() limits.Limits {
	return limits.Limits{
		MaxStreamBytes:       l.MaxStreamBytes,
		MaxImagePixels:       l.MaxImagePixels,
		MaxEmbeddedFileBytes: l.MaxEmbeddedFileBytes,
		MaxFormDepth:         l.MaxFormDepth,
		MaxCachedObjects:     l.MaxCachedObjects,
		MaxRenderPixels:      l.MaxRenderPixels,
	}
}

// limitsFromInternal translates the internal budgets to [Limits].
func limitsFromInternal(l limits.Limits) Limits {
	return Limits{
		MaxStreamBytes:       l.MaxStreamBytes,
		MaxImagePixels:       l.MaxImagePixels,
		MaxEmbeddedFileBytes: l.MaxEmbeddedFileBytes,
		MaxFormDepth:         l.MaxFormDepth,
		MaxCachedObjects:     l.MaxCachedObjects,
		MaxRenderPixels:      l.MaxRenderPixels,
	}
}
