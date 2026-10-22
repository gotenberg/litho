package limits

import "fmt"

// Limits holds the six resource budgets of one document. A zero field stands
// for its default, which [Limits.WithDefaults] fills in. Only these six are
// configurable: the recursion guards that protect litho's own stack stay fixed
// consts in the packages that recurse, since a Go stack overflow is fatal and
// no recover can catch it.
//
// Limits is a plain value. A Document is given one during construction and
// only reads it afterwards, so it needs no lock.
type Limits struct {
	// MaxStreamBytes bounds the decoded output of one stream. See
	// [DefaultMaxStreamBytes]. The page content bound, the object-stream
	// cache, the font-program cache, the colour-transform cache and the form
	// content cache derive from it: see [Limits.MaxPageContentBytes],
	// [Limits.MaxObjStmCacheBytes], [Limits.MaxFontProgCacheBytes],
	// [Limits.MaxColorCacheBytes] and [Limits.MaxFormCacheBytes].
	MaxStreamBytes int64

	// MaxImagePixels bounds Width x Height of one image, and scales the
	// codecs' pixel and sample caps. See [DefaultMaxImagePixels].
	MaxImagePixels int64

	// MaxEmbeddedFileBytes bounds the decoded embedded PDF compliance opens.
	// See [DefaultMaxEmbeddedFileBytes].
	MaxEmbeddedFileBytes int64

	// MaxFormDepth bounds how many form XObjects nest below the page. See
	// [DefaultMaxFormDepth].
	MaxFormDepth int

	// MaxCachedObjects bounds the entries of the object cache. See
	// [DefaultMaxCachedObjects].
	MaxCachedObjects int

	// MaxRenderPixels bounds Width x Height of one rendered page and of every
	// raster the renderer allocates at its size. See
	// [DefaultMaxRenderPixels].
	MaxRenderPixels int64
}

// DefaultMaxStreamBytes caps the decoded output of one stream at 256 MiB. It is
// an anti-DoS guard, not spec-mandated: ISO 32000-1, 7.4 puts no bound on a
// filter's output, so a few kilobytes of FlateDecode data can expand into
// gigabytes, and RunLengthDecode repeats one byte up to 128 times for every two
// it reads (7.4.5).
//
// It bounds the output of each FlateDecode, LZWDecode, BrotliDecode,
// RunLengthDecode and CCITTFaxDecode filter in a chain, so the chain's output
// too, and the output of a filter plugin. It bounds a linearization hint stream
// as well (ISO 32000-1, Annex F), which package document decodes with its own
// zlib reader. The image codecs are bounded by the pixel budget instead, see
// [DefaultMaxImagePixels].
//
// 256 MiB leaves about ten times the largest output the capped filters produce
// for real documents.
//
// # Corpus evidence
//
//   - All corpora: the largest FlateDecode, LZWDecode or BrotliDecode output of
//     the 5163 files that open is 26,614,380 bytes (pdfjs images.pdf, one
//     FlateDecode stream).
//   - pdfjs issue17190_1.pdf: the one stream past the cap, a 19522x27655 image
//     of one byte per pixel that reaches 539,880,910 bytes through FlateDecode
//     then RunLengthDecode. Its FlateDecode stage yields 19,201,892 bytes, so
//     the RunLengthDecode stage trips the cap, and the image budget refuses the
//     image before any decode.
//   - pdfjs issue17190.pdf: the largest CCITTFaxDecode output is 69,749,526
//     bytes.
//   - All corpora: 849 files carry a hint stream, and the largest FlateDecode
//     one decodes to 51,715 bytes (pdfjs bug810214.pdf).
const DefaultMaxStreamBytes = 256 << 20

// DefaultMaxImagePixels caps the Width x Height one image may declare before
// litho decodes it at 100 million pixels. It is an anti-DoS guard, not
// spec-mandated: ISO 32000-1, 8.9.5, Table 89 puts no bound on /Width or
// /Height, and a dictionary declaring 100,000 x 100,000 pixels asks for about
// 40 GB. 100 megapixels covers print-resolution photographs, where an 8K
// display holds about 33.
//
// The image decoder reads at most four bytes per pixel of decoded samples, the
// RGBA worst case. The codecs cap what a codestream declares whatever its
// dictionary says, and those caps scale with the budget: see
// [Limits.ScaleImageCap].
const DefaultMaxImagePixels = 100_000_000

// DefaultMaxEmbeddedFileBytes caps the decoded embedded PDF PDF/A validation
// opens at 50 MiB. A larger embedded PDF is not validated and raises no
// violation, so a crafted attachment cannot make validation hold a huge
// document in memory and parse it. It is an anti-DoS guard, not
// spec-mandated: ISO 32000-1, 7.11.4 puts no bound on an embedded file stream.
//
// The cap does not spare a large file that is not a PDF: its header decides
// that, and compliance reads only the header to find out.
//
// 50 MiB leaves better than nine times the largest embedded file in real
// documents, and better than sixty times the largest embedded PDF.
//
// # Corpus evidence
//
//   - All corpora: 275 files embed 552 files, the largest 5,599,569 bytes
//     and not a PDF (verapdf PDF_A-4__veraPDF_test_suite_6-3-1-t01-fail-e.pdf
//     and three PDF_A-4e siblings).
//   - facturx xmlrechnung__FX_EN16931_Elektron.pdf,
//     xmlrechnung__FX_XRECHNUNG_Elektron.pdf and
//     v2__correct_intarsys_EN16931_zugferd_2p0_EN16931_Elektron.pdf: the
//     largest embedded PDF, 843,023 bytes.
const DefaultMaxEmbeddedFileBytes = 50 << 20

// DefaultMaxFormDepth caps how many form XObjects nest below the page at 10. A
// form the page paints directly is at depth 1, a form that form paints at depth
// 2, and an annotation's appearance stream counts as a form the page paints.
// It is an anti-DoS guard, not spec-mandated: ISO 32000-1, 8.10 puts no limit
// on nesting, and every level costs work in each walk that reads it.
//
// It bounds the invocation chain only. Every walk breaks a cycle with a visited
// set of its own, since a depth bound survives a cycle without detecting it.
//
// Text extraction, image listing, compliance validation and auto-tagging read
// the same budget, so a form too deep for one of them is too deep for all.
// Compliance also counts a tiling pattern and a Type 3 glyph description as a
// level. Real documents nest far shallower: a form invoking a form is already
// uncommon.
//
// # Corpus evidence
//
//   - All corpora: of the 5157 files whose text extraction runs, 1315 invoke
//     a form and 47 a form inside a form. Outside two veraPDF q/Q nesting
//     fixtures the deepest chain is 4 (pdfjs issue14562.pdf).
//   - All corpora: of the images the 5163 files that open list, 84,304 sit at
//     depth 0, 3,528 at depth 1, 1,398 at depth 2, 61 at depth 3 and 9 at
//     depth 4, so image listing, which read 16 levels, lists the same images
//     under this budget.
//   - verapdf PDF_A-1b__veraPDF_test_suite_6-1-12-t08-pass-c.pdf: the only
//     file the cap cuts short, a chain of 33 forms whose innermost form paints
//     a rectangle and no text. Its sibling pass-a.pdf nests 9 deep.
const DefaultMaxFormDepth = 10

// DefaultMaxCachedObjects is the number of parsed objects a document keeps,
// 50,000, before its object cache is emptied and starts again. It is an
// anti-DoS guard, not spec-mandated: the object count comes from the file, and
// a crafted file with millions of objects would otherwise keep every one the
// resolver parsed.
//
// Eviction costs a re-parse, never a wrong answer, so the value only has to
// cover the object table of ordinary documents. Fifty thousand holds every
// object of all but rare outliers, which then evict rather than grow.
//
// # Corpus evidence
//
//   - All corpora: 5162 of the 5163 files that open hold at most 50,000
//     objects, so their cache never evicts. The median holds 20 and the
//     next largest 37,864 (pdfjs issue19281.pdf).
//   - pdfjs bug1978317.pdf: the one file past the cap, 65,563 objects.
const DefaultMaxCachedObjects = 50_000

// DefaultMaxRenderPixels caps the pixels of one rendered page at 50 million.
// It is an anti-DoS guard, not spec-mandated: the page size comes from the
// file (ISO 32000-1, 7.7.3.3, Table 30 lets a box span 14,400 units and
// /UserUnit scale it further) and the resolution from the caller, and the
// renderer holds 16 bytes a pixel for the page and 4 for each clip it keeps,
// so an unchecked product is gigabytes.
//
// It bounds the page and every raster the renderer allocates at its size:
// clip masks and the coverage of a pattern fill. 50 million pixels holds a US
// Letter or A4 page at 600 dpi, 33.7 and 34.8 million, for 800 MB of page
// raster.
const DefaultMaxRenderPixels = 50_000_000

// Ceilings bound what a caller may configure. Past its ceiling a budget stops
// being a guard: one operation would hold more memory than a host has, or the
// arithmetic built on the budget would leave the int64 range.
const (
	// MaxStreamBytesCeiling is 4 GiB, sixteen times the default. One decode
	// holds its whole output in memory.
	MaxStreamBytesCeiling = 4 << 30

	// MaxImagePixelsCeiling is 2^30 pixels. The decoder reads up to four
	// bytes a pixel, so the samples of one image then reach the stream
	// ceiling, and every codec cap [Limits.ScaleImageCap] scales stays well
	// inside int64.
	MaxImagePixelsCeiling = 1 << 30

	// MaxEmbeddedFileBytesCeiling is 4 GiB, the stream ceiling: an embedded
	// PDF is held whole in memory while it is validated.
	MaxEmbeddedFileBytesCeiling = 4 << 30

	// MaxFormDepthCeiling is 256, the depth guard of the page tree. Each level
	// costs a few stack frames in every walk that reads it, which keeps the
	// deepest walk far from the goroutine stack limit.
	MaxFormDepthCeiling = 256

	// MaxCachedObjectsCeiling is 8,388,607, the most indirect objects a PDF
	// file holds under ISO 32000-1, Annex C, Table C.1, so a larger cache
	// never fills.
	MaxCachedObjectsCeiling = 8_388_607

	// MaxRenderPixelsCeiling is 2^30 pixels, the image ceiling. The page
	// raster then holds 16 GiB, past what one render can reasonably ask of a
	// host.
	MaxRenderPixelsCeiling = 1 << 30
)

// Default returns every budget at its default.
func Default() Limits {
	return Limits{
		MaxStreamBytes:       DefaultMaxStreamBytes,
		MaxImagePixels:       DefaultMaxImagePixels,
		MaxEmbeddedFileBytes: DefaultMaxEmbeddedFileBytes,
		MaxFormDepth:         DefaultMaxFormDepth,
		MaxCachedObjects:     DefaultMaxCachedObjects,
		MaxRenderPixels:      DefaultMaxRenderPixels,
	}
}

// WithDefaults returns l with every zero budget set to its default.
func (l Limits) WithDefaults() Limits {
	d := Default()
	if l.MaxStreamBytes == 0 {
		l.MaxStreamBytes = d.MaxStreamBytes
	}
	if l.MaxImagePixels == 0 {
		l.MaxImagePixels = d.MaxImagePixels
	}
	if l.MaxEmbeddedFileBytes == 0 {
		l.MaxEmbeddedFileBytes = d.MaxEmbeddedFileBytes
	}
	if l.MaxFormDepth == 0 {
		l.MaxFormDepth = d.MaxFormDepth
	}
	if l.MaxCachedObjects == 0 {
		l.MaxCachedObjects = d.MaxCachedObjects
	}
	if l.MaxRenderPixels == 0 {
		l.MaxRenderPixels = d.MaxRenderPixels
	}
	return l
}

// Validate reports the first budget that is negative or above its ceiling, as
// an error wrapping [ErrInvalid]. A zero budget is valid: it stands for its
// default.
func (l Limits) Validate() error {
	for _, b := range []struct {
		name           string
		value, ceiling int64
	}{
		{"MaxStreamBytes", l.MaxStreamBytes, MaxStreamBytesCeiling},
		{"MaxImagePixels", l.MaxImagePixels, MaxImagePixelsCeiling},
		{"MaxEmbeddedFileBytes", l.MaxEmbeddedFileBytes, MaxEmbeddedFileBytesCeiling},
		{"MaxFormDepth", int64(l.MaxFormDepth), MaxFormDepthCeiling},
		{"MaxCachedObjects", int64(l.MaxCachedObjects), MaxCachedObjectsCeiling},
		{"MaxRenderPixels", l.MaxRenderPixels, MaxRenderPixelsCeiling},
	} {
		if b.value < 0 {
			return fmt.Errorf("%w: %s is %d, below zero", ErrInvalid, b.name, b.value)
		}
		if b.value > b.ceiling {
			return fmt.Errorf("%w: %s is %d, above its ceiling of %d", ErrInvalid, b.name, b.value, b.ceiling)
		}
	}
	return nil
}

// ScaleImageCap returns base, a codec's pixel or sample cap under the default
// image budget, scaled by MaxImagePixels over [DefaultMaxImagePixels], and
// never below 1. A zero MaxImagePixels reads as its default, so the default
// configuration returns base exactly and every codec admits the images it
// always admitted, while a raised or lowered budget moves each codec cap with
// it. The product stays within int64 for any base up to 2^32, the codec caps
// being 4 x 10^8 samples at most, under [MaxImagePixelsCeiling].
func (l Limits) ScaleImageCap(base int64) int64 {
	pixels := l.WithDefaults().MaxImagePixels
	return max(base*pixels/DefaultMaxImagePixels, 1)
}

// pageContentShare divides the stream budget into the page content bound:
// [Limits.MaxPageContentBytes] is MaxStreamBytes over it. That method says why
// a page gets a quarter.
const pageContentShare = 4

// MaxPageContentBytes returns the bound on the decoded content one page, or one
// form XObject, accumulates across its /Contents array: a quarter of
// MaxStreamBytes and never below 1, so 64 MiB under the default budget. It is
// an anti-DoS guard, not spec-mandated.
//
// ISO 32000-1, 7.7.3.3 lets a page split its content across an array of
// streams, and MaxStreamBytes bounds each part but not their sum: a page can
// name thousands of stream objects, and without this bound the accumulated
// buffer grows with the file's object count rather than with anything the page
// could draw. Every reader of page content applies this one bound (text
// extraction, compliance, remediation, auto-tagging, stamping and redaction),
// and the redaction scan applies it to each stream it reads, so no reader
// refuses a page another reads.
//
// It moves with MaxStreamBytes: a caller that raises the stream budget to read
// a heavy page reads it whole, and one that lowers the budget bounds pages too.
// It is a quarter of the budget, not the whole of it, because a page is held
// twice over, as bytes and then as the operators parsed from them, which retain
// about five times the bytes. A page at a quarter of the budget holds about one
// and a half stream budgets once parsed, where a page as large as the budget
// would hold over six.
//
// # Corpus evidence
//
//   - All corpora: the largest real page found across all of them is page 1 of
//     pdfjs issue21089.pdf, at 27.51 MB of decoded content in a 12.11 MB file,
//     so the default leaves better than twice its size.
//   - pdfjs issue21089.pdf: page 1 decodes to 28,846,545 bytes across 55
//     streams and parses into 608,043 operators that retain 152 MB, 5.3 bytes
//     per content byte.
//   - pdfjs issue21089.pdf: page 1 came within 16% of the former 32 MB bound.
func (l Limits) MaxPageContentBytes() int64 {
	return max(l.WithDefaults().MaxStreamBytes/pageContentShare, 1)
}

// MaxObjStmCacheBytes returns the bound on the decoded object-stream bytes a
// document keeps cached: MaxStreamBytes itself, so 256 MiB under the default
// budget. It is an anti-DoS guard, not spec-mandated: a crafted file can pack
// many large object streams (ISO 32000-1, 7.5.7), so the cache empties rather
// than grows without bound.
//
// It equals the stream budget because that budget bounds each object stream the
// cache holds. Every object stream that decodes then fits, and the cache never
// holds more than one worst-case decode. A bound below the budget would leave
// the streams between the two uncached, so each object read from one decodes
// the whole stream again. A bound above it would let the cache outgrow what the
// caller allowed one decode.
func (l Limits) MaxObjStmCacheBytes() int64 {
	return l.WithDefaults().MaxStreamBytes
}

// fontProgCacheShare divides the stream budget into the font-program cache
// bound: [Limits.MaxFontProgCacheBytes] is MaxStreamBytes over it. That method
// says why the cache gets a quarter.
const fontProgCacheShare = 4

// MaxFontProgCacheBytes returns the bound on the decoded font programs a
// document keeps cached: a quarter of MaxStreamBytes and never below 1, so 64
// MiB under the default budget. It is an anti-DoS guard, not spec-mandated: a
// crafted file can embed many large font programs (ISO 32000-1, 9.9), so the
// cache empties rather than grows without bound. A program larger than the
// bound is not cached and is decoded again at each use, which costs time,
// never a wrong answer.
//
// It moves with MaxStreamBytes, which bounds each program the cache holds: a
// caller that raises the budget to read a document of heavy fonts caches them
// too, and one that lowers it shrinks the cache. It is a quarter of the
// budget, not the whole of it as the object-stream cache is, because the
// default then stays the 64 MiB the cache always had, well over the fonts of
// the heaviest real document, and the document's caches together stay within
// a small multiple of one budget.
//
// # Corpus evidence
//
//   - All corpora: of the 2129 documents that embed any font program, the
//     heaviest embeds 18.4 MB of them, so 64 MiB leaves better than 3x
//     headroom.
func (l Limits) MaxFontProgCacheBytes() int64 {
	return max(l.WithDefaults().MaxStreamBytes/fontProgCacheShare, 1)
}

// colorCacheShare divides the stream budget into the colour-transform cache
// bound: [Limits.MaxColorCacheBytes] is MaxStreamBytes over it. That method
// says why the cache gets a quarter.
const colorCacheShare = 4

// MaxColorCacheBytes returns the bound on the colour transforms a document
// keeps cached, built from its ICC profile streams: a quarter of
// MaxStreamBytes and never below 1, so 64 MiB under the default budget. It is
// an anti-DoS guard, not spec-mandated: a crafted file can declare many
// ICCBased colour spaces (ISO 32000-1, 8.6.5.5) whose profiles carry large
// lookup tables, so the cache empties rather than grows without bound. A
// transform larger than the bound is not cached and is built again at each
// use, which costs time, never a wrong answer.
//
// It moves with MaxStreamBytes, which bounds each profile stream a transform
// is built from: a caller that raises the budget to read a document of heavy
// profiles caches their transforms too, and one that lowers it shrinks the
// cache. It is a quarter of the budget, as the font-program cache is, because
// the default then stays the 64 MiB the cache always had. The densest lookup
// table in common use, 33 points over four inputs, builds a transform of
// about 29 MB, and a document rarely holds more than one or two profiles.
func (l Limits) MaxColorCacheBytes() int64 {
	return max(l.WithDefaults().MaxStreamBytes/colorCacheShare, 1)
}

// formCacheShare divides the stream budget into the form content cache bound:
// [Limits.MaxFormCacheBytes] is MaxStreamBytes over it. That method says why
// the cache gets a quarter.
const formCacheShare = 4

// MaxFormCacheBytes returns the bound on the parsed form XObject content a
// document keeps cached, counted in the bytes the parsed operators retain: a
// quarter of MaxStreamBytes and never below 1, so 64 MiB under the default
// budget. It is an anti-DoS guard, not spec-mandated: a crafted file can
// define many large form XObjects (ISO 32000-1, 8.10), so the cache empties
// rather than grows without bound. A form larger than the bound is not cached
// and is parsed again at each use, which costs time, never a wrong answer.
//
// It moves with MaxStreamBytes, which bounds the content of each form the
// cache holds: a caller that raises the budget to read a document of heavy
// forms caches them too, and one that lowers it shrinks the cache. It is a
// quarter of the budget, as the font-program and colour-transform caches are,
// so the document's caches together stay within a small multiple of one
// budget. A parsed operator retains about five times the content bytes it was
// read from (see [Limits.MaxPageContentBytes]), so the default holds about 12
// MiB of form content.
//
// # Corpus evidence
//
//   - pdfjs issue19281.pdf: extracting the text of its 350 pages before and
//     after flattening spent 2.0 s of 9.9 s CPU parsing form XObjects, the
//     same forms again on each page; with the cache, all content parsing
//     takes 0.2 s.
func (l Limits) MaxFormCacheBytes() int64 {
	return max(l.WithDefaults().MaxStreamBytes/formCacheShare, 1)
}
