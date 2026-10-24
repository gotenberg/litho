package mutations

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// isImageXObjectDict reports whether a stream dictionary declares its data an
// image's samples: an image XObject (ISO 32000-1, 8.9.5, Table 89), whose
// /Subtype is /Image and whose optional /Type is /XObject. A stencil or soft
// mask (8.9.6.3, 11.6.5.2) and a page thumbnail (12.3.4) are image XObjects
// too. A dictionary whose /Type names something else contradicts itself and is
// not trusted to be one.
func isImageXObjectDict(dict ast.PdfDict) bool {
	if subtype, _ := dict["Subtype"].(ast.PdfName); subtype != "Image" {
		return false
	}
	typ, hasType := dict["Type"].(ast.PdfName)
	return !hasType || typ == "XObject"
}

// imageDataSkipped reports whether the decoded data of an image XObject is
// left out of the comparison, and records the stream so a later reference
// that reads it as something else scans it after all (see
// [scanWalk.markReadAsData]).
//
// An image's data is its samples, read as colour values by the image's
// colour space (ISO 32000-1, 8.9.2 and 8.9.5), whatever filter compresses
// them: DCTDecode, JPXDecode, JBIG2Decode and CCITTFaxDecode are image codecs
// (7.4.8 to 7.4.10, 7.4.6), and a Flate- or uncompressed image holds raw
// samples. Text extraction reads character codes from content streams (9.10.2)
// and never an image's samples, so a removed word the bytes of an image spell
// is pixels or entropy-coded data that happen to match, a copy no reader
// finds. Comparing them refused redactions over three-letter tokens such as
// "www" that the compressed data of an image another page draws holds by
// chance. Metadata a codec carries inside its stream, a JPEG comment or a JPX
// XML box, is not read as page text either, as a font program's /Notice is not
// (see [binarySlotKeys]). An image's /Metadata stream (14.3.2) is a stream of
// its own and is scanned as usual.
//
// The stored bytes past the first filter's end are still compared (see
// [scanWalk.scanStoredTail]): they are no samples, and a reader of the file's
// bytes finds them. The image codecs report no such tail.
//
// The skip trusts the dictionary, which is what a reader paints the stream by,
// but not against every reader: a page's /Contents, a file specification's
// embedded file, a /Metadata, a JavaScript action's /JS and a form's /XFA read
// a stream as bytes whatever its dictionary says, so a stream any of them
// names is compared, whichever reference the walk meets first.
//
// # Corpus evidence
//
//   - govdocs, pdfjs, pdfcpu, litho, redacting page 1 whole, its centred
//     quarter and a band (7,692 redactions): the data of an image held a
//     surviving copy in 114 refusals with ErrRedactionIncomplete, 89 govdocs,
//     22 pdfjs, 3 pdfcpu, none litho. Skipping it lets 49 through (32, 16,
//     1), and 59 under the raster fallback; the others refuse over other
//     copies. No other outcome moves.
//   - pdfcpu FOSDEM14_HPC_devroom_14_GoCUDA.pdf, the quarter: the one
//     redaction that moves to another refusal, the pixel check's, over 3
//     pixels at the region's edge it had not reached before.
//   - govdocs, pdfjs, pdfcpu: of 1,464 matches in JPEG images, none lay in
//     a comment or application segment. 6 lay in entropy-coded bytes, and
//     the other 1,458 spell nothing in the stored bytes: the scan matched
//     them in the samples it decoded. Raw-sample (Flate, LZW) and JPX images
//     held 46 and 49 more.
//   - pdfjs issue9367.pdf: an attached PDF whose JPEG samples spelled "EUR".
//   - govdocs, timed back to back at load 10: the scan decoded every image,
//     so Redact and WriteTo over 3,273 redactions took 510 s instead of
//     1,024 s, the 95th percentile 504 ms instead of 1,153.
func (w *scanWalk) imageDataSkipped(objNum int, stream ast.PdfStream) bool {
	if !isImageXObjectDict(stream.Dict) || w.contents[objNum] || w.readAsData[objNum] {
		return false
	}
	w.images[objNum] = stream
	return true
}

// markReadAsData records the streams dict names in a slot that reads a stream
// as bytes, whatever its own dictionary says: an embedded file stream of a
// file specification's /EF (ISO 32000-1, 7.11.4, Table 44), a metadata stream
// (14.3.2), a JavaScript action's /JS (12.6.4.16, Table 217) and an
// interactive form's /XFA, a stream or an array of streams (12.7.8, Table
// 218). An image XObject whose data the scan skipped and that one of them
// names is scanned now.
func (w *scanWalk) markReadAsData(ctx context.Context, dict ast.PdfDict) {
	for _, key := range []ast.PdfName{"Metadata", "JS", "XFA", "EF"} {
		value, ok := dict[key]
		if !ok {
			continue
		}
		switch key {
		case "EF":
			for _, entry := range w.liveDict(ctx, value) {
				w.markStreamReadAsData(ctx, entry)
			}
		case "XFA":
			if arr, isArr := value.(ast.PdfArray); isArr {
				for _, item := range arr {
					w.markStreamReadAsData(ctx, item)
				}
				continue
			}
			w.markStreamReadAsData(ctx, value)
		default:
			w.markStreamReadAsData(ctx, value)
		}
	}
}

// markStreamReadAsData records one reference of a slot that reads its stream
// as bytes, and scans the data of an image XObject the walk skipped under it.
func (w *scanWalk) markStreamReadAsData(ctx context.Context, obj ast.PdfObject) {
	ref, isRef := obj.(ast.PdfIndirectRef)
	if !isRef || w.readAsData[ref.ObjectNum] {
		return
	}
	w.readAsData[ref.ObjectNum] = true
	w.rescanSkippedImage(ctx, ref.ObjectNum)
}

// rescanSkippedImage scans the data of an image XObject whose data the walk
// skipped, once a reference proves a reader reads it as something else too.
func (w *scanWalk) rescanSkippedImage(ctx context.Context, objNum int) {
	stream, ok := w.images[objNum]
	if !ok {
		return
	}
	delete(w.images, objNum)
	w.scanStreamData(ctx, objNum, stream)
}
