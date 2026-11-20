package models

// Limits bounds the resources litho spends on one document: how many bytes a
// stream may decode to, how many pixels an image may hold, how large an
// embedded PDF validation opens, how deeply form XObjects nest, how many
// parsed objects stay cached, how many pixels a rendered page may hold, and
// how large a source a [github.com/gotenberg/litho/pkg/pdf.Conversion] reads,
// how many pages it lays out and how much memory it holds. Each budget is an
// anti-DoS guard, not a requirement of ISO 32000: a few kilobytes of crafted
// input can otherwise ask for gigabytes of memory or unbounded work. The
// defaults ([github.com/gotenberg/litho/pkg/pdf.DefaultLimits]) sit far above
// what real documents need.
//
// A zero field keeps its default, so Limits{MaxStreamBytes: 1 << 30} raises
// one budget and leaves the others alone. A negative field, or one above its
// ceiling, makes [github.com/gotenberg/litho/pkg/pdf.Open] and
// [github.com/gotenberg/litho/pkg/pdf.Conversion.WriteTo] fail with
// [github.com/gotenberg/litho/pkg/pdf.ErrInvalidLimits], and
// [github.com/gotenberg/litho/pkg/pdf.ValidateLimits] reports the same error
// without opening a document. Pass it to
// [github.com/gotenberg/litho/pkg/pdf.Open] or
// [github.com/gotenberg/litho/pkg/pdf.OpenFile] with
// [github.com/gotenberg/litho/pkg/pdf.WithLimits], and to a
// [github.com/gotenberg/litho/pkg/pdf.Conversion] with
// [github.com/gotenberg/litho/pkg/pdf.WithConvertLimits].
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
// Limits is a plain value, safe to copy and share.
// [github.com/gotenberg/litho/pkg/pdf.Open] reads it once, and the
// [github.com/gotenberg/litho/pkg/pdf.Reader] applies it for its whole life,
// to every document a [github.com/gotenberg/litho/pkg/pdf.Writer] or
// [github.com/gotenberg/litho/pkg/pdf.Pipeline] re-reads from it too.
type Limits struct {
	// MaxStreamBytes bounds the decoded bytes of one stream: the output of
	// each FlateDecode, LZWDecode, BrotliDecode, RunLengthDecode and
	// CCITTFaxDecode filter in its chain, of a filter plugin, and of a
	// linearization hint stream. A stream past it fails to decode with
	// [github.com/gotenberg/litho/pkg/pdf.ErrStreamTooLarge]:
	// [github.com/gotenberg/litho/pkg/pdf.Reader.DecodeImage], an image's
	// Decode function and the Open function of an image or an embedded file
	// return that error, and a stream litho reads for its own use, such as a
	// content stream, is skipped as a damaged one would be. The image codecs,
	// DCTDecode, JPXDecode and JBIG2Decode, are bounded by MaxImagePixels
	// instead. An Open function streams a single FlateDecode, LZWDecode or
	// BrotliDecode filter without holding its output, and a read past the
	// bound fails with [github.com/gotenberg/litho/pkg/pdf.ErrStreamTooLarge]
	// as well. Default 256 MiB, at most 4 GiB.
	//
	// Four further bounds move with it. The content of one page, summed across
	// the streams of its /Contents array, may take a quarter of it, 64 MiB by
	// default: text extraction, validation, remediation, auto-tagging,
	// stamping and redaction refuse a page with more, as they refuse a damaged
	// one, and a call that fails on it returns
	// [github.com/gotenberg/litho/pkg/pdf.ErrPageContentTooLarge]. The
	// redaction scan reads no more than that from any one stream. A quarter
	// and not the whole, because the operators parsed from a page hold several
	// times its bytes: about three and a half on a very large page, up to ten
	// on a page of short operators. The decoded object streams the
	// [github.com/gotenberg/litho/pkg/pdf.Reader] keeps cached may take the
	// whole of it, and the decoded font programs and the colour transforms
	// built from ICC profiles it keeps cached a quarter each, 64 MiB by
	// default: when one would take more, its cache is emptied and refilled,
	// which costs re-decoding, never a wrong answer.
	MaxStreamBytes int64 `json:"maxStreamBytes"`

	// MaxImagePixels bounds Width x Height of one image.
	// [github.com/gotenberg/litho/pkg/pdf.Reader.DecodeImage] and an image's
	// Decode function refuse a larger image before decoding it, and decoded
	// samples past four bytes a pixel of it, with
	// [github.com/gotenberg/litho/pkg/pdf.ErrImageTooLarge]. The sample caps
	// of the DCTDecode and JPXDecode codecs scale with it, so a codestream is
	// held to the same budget whatever its dictionary declares. JBIG2Decode,
	// one bit a pixel, is held to its bytes: a bitmap may take a byte for each
	// pixel of the budget, so a mask past it, which is read reduced to the
	// size it is drawn at, still decodes. A
	// [github.com/gotenberg/litho/pkg/pdf.Conversion] holds each picture of
	// its source to it as well: a larger one is left out with a warning.
	// Default 100,000,000, at most 2^30.
	MaxImagePixels int64 `json:"maxImagePixels"`

	// MaxEmbeddedFileBytes bounds the decoded size of an embedded PDF that
	// PDF/A validation opens to check its conformance (ISO 19005-2, 6.8;
	// ISO 19005-4, 6.9). A larger embedded PDF is not validated and raises no
	// violation. An embedded file that is not a PDF is judged from its header
	// and reported whatever its size. Default 50 MiB, at most 4 GiB.
	MaxEmbeddedFileBytes int64 `json:"maxEmbeddedFileBytes"`

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
	MaxFormDepth int `json:"maxFormDepth"`

	// MaxCachedObjects bounds how many parsed objects the
	// [github.com/gotenberg/litho/pkg/pdf.Reader] keeps. When the cache is
	// full, an object not used lately makes room for the next one, which costs
	// parsing it again if it is asked for, never a wrong answer. Default
	// 50,000, at most 8,388,607, the most indirect objects ISO 32000-1, Annex
	// C allows a file.
	MaxCachedObjects int `json:"maxCachedObjects"`

	// MaxRenderPixels bounds Width x Height of one page
	// [github.com/gotenberg/litho/pkg/pdf.Reader.RenderPage] draws, and of
	// every raster it allocates at the page's size, such as a clip. A page
	// whose size at the requested resolution exceeds it fails with
	// [github.com/gotenberg/litho/pkg/pdf.ErrRenderTooLarge] before anything
	// is drawn. The renderer holds 16 bytes a pixel for the page. Default
	// 50,000,000, a US Letter or A4 page at 600 dpi, at most 2^30.
	MaxRenderPixels int64 `json:"maxRenderPixels"`

	// MaxConvertInputBytes bounds the source bytes one
	// [github.com/gotenberg/litho/pkg/pdf.Conversion] reads. A larger source
	// fails with [github.com/gotenberg/litho/pkg/pdf.ErrConvertInputTooLarge]
	// and is never truncated. A conversion holds the source and the laid-out
	// document in memory, about 50 bytes for each character of plain text and
	// 170 for each byte of HTML, within MaxConvertMemoryBytes. Default 16 MiB,
	// about 4,200 A4 pages of plain text at 12 points, at most 1 GiB.
	MaxConvertInputBytes int64 `json:"maxConvertInputBytes"`

	// MaxConvertPages bounds the pages one
	// [github.com/gotenberg/litho/pkg/pdf.Conversion] lays out. A source that
	// needs more fails with
	// [github.com/gotenberg/litho/pkg/pdf.ErrConvertTooManyPages]; no page is
	// dropped. Default 10,000, at most 100,000.
	MaxConvertPages int `json:"maxConvertPages"`

	// MaxConvertMemoryBytes bounds the memory one
	// [github.com/gotenberg/litho/pkg/pdf.Conversion] holds, as litho accounts
	// it: the source, the inflated parts of a package and the XML trees built
	// from them, the HTML tree, the boxes of the layout and the computed style
	// each holds, the shaped lines, the cells of a sheet, the shapes of a
	// slide, the decoded pictures and effect bitmaps while they live, the font
	// programs an HTML source's @font-face rules load, and the pages and
	// streams it writes. The index of the installed fonts, and the faces it
	// keeps parsed, are shared by the process and charged to no conversion:
	// [github.com/gotenberg/litho/pkg/pdf.WithConvertFontDirs] says what they
	// hold. Each is charged before it is taken, so a conversion past the
	// budget fails with
	// [github.com/gotenberg/litho/pkg/pdf.ErrConvertInputTooLarge], naming
	// what was being held, before it takes the memory and without finishing
	// its work. Nothing is written.
	//
	// The account follows what a conversion keeps alive, measured per
	// format: plain text costs about 50 bytes a source byte, 190 KiB a page;
	// HTML about 170 bytes a source byte, 690 KiB a page; a DOCX of short
	// pages about 80 KiB a page; a sheet about 1.8 KiB a cell; a picture
	// 4 bytes a pixel (8 for 16 bits a channel) while it is decoded, then
	// its compressed samples, and a JPEG written as it is its file. So the
	// default holds a text source at the default MaxConvertInputBytes, and
	// refuses an HTML source of more than about 6 MiB of plain paragraphs
	// and a workbook of more than about 600,000 cells. The Go heap peaks at
	// about twice the account, since the garbage collector frees what is
	// released a while later: size a host for twice the budget per
	// concurrent conversion. Default 1 GiB, at most 64 GiB.
	MaxConvertMemoryBytes int64 `json:"maxConvertMemoryBytes"`
}
