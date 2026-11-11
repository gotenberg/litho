package pdf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// Limits bounds the resources litho spends on one document: how many bytes a
// stream may decode to, how many pixels an image may hold, how large an
// embedded PDF validation opens, how deeply form XObjects nest, how many
// parsed objects stay cached, how many pixels a rendered page may hold, and
// how large a source [Convert] reads, how many pages it lays out and how
// much memory it holds. Each
// budget is an anti-DoS guard, not a requirement of ISO 32000: a few kilobytes
// of crafted input can otherwise ask for gigabytes of memory or unbounded
// work. The defaults ([DefaultLimits]) sit far above what real documents need.
//
// A zero field keeps its default, so Limits{MaxStreamBytes: 1 << 30} raises
// one budget and leaves the others alone. A negative field, or one above its
// ceiling, makes [Open] and [Convert] fail with [ErrInvalidLimits], and
// [Limits.Validate] reports the same error without opening a document. Pass it
// to [Open] or [OpenFile] with [WithLimits], and to [Convert] with
// [WithConvertLimits].
//
// The ceilings below hold on a 64-bit platform. On a 32-bit one, where a Go
// int, and with it any one buffer, stops below 2 GiB, they are lower:
// MaxStreamBytes and MaxEmbeddedFileBytes at most 2 GiB - 1, MaxImagePixels at
// most 2^29 - 1 and MaxRenderPixels at most 2^27 - 1. The defaults are the same
// on both.
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
	// BrotliDecode filter without holding its output, and a read past the
	// bound fails with [ErrStreamTooLarge] as well. Default 256 MiB, at most
	// 4 GiB.
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
	// decodes. [Convert] holds each picture of its source to it as well: a
	// larger one is left out with a warning. Default 100,000,000, at most
	// 2^30.
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

	// MaxConvertInputBytes bounds the source bytes one [Convert] call reads.
	// A larger source fails with [ErrConvertInputTooLarge] and is never
	// truncated. Convert holds the source and the laid-out document in
	// memory, about 50 bytes for each character of plain text and 170 for
	// each byte of HTML, within MaxConvertMemoryBytes. Default 16 MiB,
	// about 4,200 A4 pages of plain text at 12 points, at most 1 GiB.
	MaxConvertInputBytes int64

	// MaxConvertPages bounds the pages one [Convert] call lays out. A source
	// that needs more fails with [ErrConvertTooManyPages]; no page is
	// dropped. Default 10,000, at most 100,000.
	MaxConvertPages int

	// MaxConvertMemoryBytes bounds the memory one [Convert] call holds, as
	// litho accounts it: the source, the inflated parts of a package and the
	// XML trees built from them, the HTML tree, the boxes of the layout and
	// the computed style each holds, the shaped lines, the cells of a sheet,
	// the shapes of a slide, the decoded pictures and effect bitmaps while
	// they live, the font programs the conversion loads, and the pages and
	// streams it writes. Each is charged before it is taken, so a conversion
	// past the budget fails with [ErrConvertInputTooLarge], naming what was
	// being held, before it takes the memory and without finishing its work.
	// Nothing is written.
	//
	// The account follows what a conversion keeps alive, measured per
	// format: plain text costs about 50 bytes a source byte, 190 KiB a page;
	// HTML about 170 bytes a source byte, 690 KiB a page; a DOCX of short
	// pages about 80 KiB a page; a sheet about 1.8 KiB a cell; a decoded
	// picture 4 bytes a pixel (8 for 16 bits a channel), a JPEG written as
	// it is nothing beside its file. So the default holds a text source at
	// the default MaxConvertInputBytes, and refuses an HTML source of more
	// than about 6 MiB of plain paragraphs, a workbook of more than about
	// 600,000 cells, and the decoded pictures past 1 GiB. The Go heap peaks
	// at two to three times the account, since the garbage collector frees
	// what is released a while later: size a host for three times the
	// budget per concurrent conversion. Default 1 GiB, at most 64 GiB.
	MaxConvertMemoryBytes int64
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
		MaxConvertInputBytes: l.MaxConvertInputBytes,
		MaxConvertPages:      l.MaxConvertPages,

		MaxConvertMemoryBytes: l.MaxConvertMemoryBytes,
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
		MaxConvertInputBytes: l.MaxConvertInputBytes,
		MaxConvertPages:      l.MaxConvertPages,

		MaxConvertMemoryBytes: l.MaxConvertMemoryBytes,
	}
}

// withConvertBudget returns ctx carrying the memory budget of one
// conversion under l, every field filled in, and the function that ends
// it, which [Convert] defers.
func withConvertBudget(ctx context.Context, l limits.Limits) (context.Context, *limits.Budget, func()) {
	return limits.WithBudget(ctx, l)
}

// convertBudgetError logs the most memory the conversion under b held, as
// its budget accounted it, at Debug on logger, and returns err, the error
// the conversion ended with, as [Convert] reports it: when the budget refused a charge, the
// refusal wrapped in [ErrConvertInputTooLarge], whatever error the
// converter saw after its context was cancelled. A converter that swallows
// a refused charge (a picture left out with a warning) still ends with
// the refusal, since the account it was refused on is not one the
// conversion may pass.
func convertBudgetError(logger *slog.Logger, b *limits.Budget, err error) error {
	refusal := b.Err()
	if logger != nil && b != nil {
		logger.Debug("convert: memory account", slog.Int64("peak", b.Peak()), slog.Int64("held", b.Held()), slog.Bool("refused", refusal != nil))
	}
	switch {
	case refusal == nil:
		return err
	case err != nil && errors.Is(err, ErrConvertInputTooLarge) && errors.Is(err, limits.ErrMemoryBudget):
		return err
	default:
		return fmt.Errorf("converting: %w: %w", ErrConvertInputTooLarge, refusal)
	}
}

// sourceByteCost is what one byte of a conversion's source costs its
// budget: the converters hold the source whole, in one []byte.
const sourceByteCost = 1

// chargedSource returns src charging b for each byte read from it, so a
// source the budget cannot hold fails as it is read.
func chargedSource(src io.Reader, b *limits.Budget) io.Reader {
	if b == nil {
		return src
	}
	return &chargedReader{r: src, b: b}
}

// chargedReader charges its budget for each byte it reads.
type chargedReader struct {
	r io.Reader
	b *limits.Budget
}

func (c *chargedReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if cerr := c.b.ChargeUnits(int64(n), sourceByteCost, "the source"); cerr != nil {
		return n, cerr
	}
	return n, err
}
