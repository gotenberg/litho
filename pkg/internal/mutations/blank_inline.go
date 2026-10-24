package mutations

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"io"
	"log/slog"
	"math"
	"reflect"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
	"github.com/gotenberg/litho/pkg/internal/heuristics"
)

// maxIndexedHival is the largest /Indexed hival ISO 32000-1, 8.6.6.3 allows:
// the colour table holds at most 256 entries.
const maxIndexedHival = 255

// inlineImageKeys maps every key ISO 32000-1, 8.9.7, Table 93 lets an inline
// image dictionary hold, abbreviated or in full, to the image dictionary key
// of 8.9.5, Table 89 it stands for. /L and /Length are the data length ISO
// 32000-2, 8.9.7, Table 91 adds. /Intent is no abbreviation: Table 89 gives
// an image the entry, and an inline image carries it unabbreviated.
//
//nolint:gochecknoglobals // static spec table, ISO 32000-1 Table 93
var inlineImageKeys = map[ast.PdfName]ast.PdfName{
	"BPC": "BitsPerComponent", "BitsPerComponent": "BitsPerComponent",
	"CS": "ColorSpace", "ColorSpace": "ColorSpace",
	"D": "Decode", "Decode": "Decode",
	"DP": "DecodeParms", "DecodeParms": "DecodeParms",
	"F": "Filter", "Filter": "Filter",
	"H": "Height", "Height": "Height",
	"IM": "ImageMask", "ImageMask": "ImageMask",
	"I": "Interpolate", "Interpolate": "Interpolate",
	"W": "Width", "Width": "Width",
	"L": "Length", "Length": "Length",
	"Intent": "Intent",
}

// inlineFilters maps the filter names an inline image may spell, abbreviated
// as ISO 32000-1, 8.9.7, Table 94 allows or in full (7.4, Table 6), to the
// full name the decoder reads.
//
//nolint:gochecknoglobals // static spec table, ISO 32000-1 Table 94
var inlineFilters = map[ast.PdfName]ast.PdfName{
	"AHx": "ASCIIHexDecode", "ASCIIHexDecode": "ASCIIHexDecode",
	"A85": "ASCII85Decode", "ASCII85Decode": "ASCII85Decode",
	"LZW": "LZWDecode", "LZWDecode": "LZWDecode",
	"Fl": "FlateDecode", "FlateDecode": "FlateDecode",
	"RL": "RunLengthDecode", "RunLengthDecode": "RunLengthDecode",
	"CCF": "CCITTFaxDecode", "CCITTFaxDecode": "CCITTFaxDecode",
	"DCT": "DCTDecode", "DCTDecode": "DCTDecode",
}

// inlineDeviceSpaces maps the device colour space names an inline image may
// give /CS, abbreviated as ISO 32000-1, 8.9.7, Table 94 allows or in full, to
// the full names, and the full names to their component counts.
//
//nolint:gochecknoglobals // static spec table, ISO 32000-1 Table 94
var (
	inlineDeviceSpaces = map[ast.PdfName]ast.PdfName{
		"G": "DeviceGray", "DeviceGray": "DeviceGray",
		"RGB": "DeviceRGB", "DeviceRGB": "DeviceRGB",
		"CMYK": "DeviceCMYK", "DeviceCMYK": "DeviceCMYK",
	}
	deviceSpaceComponents = map[ast.PdfName]int{"DeviceGray": 1, "DeviceRGB": 3, "DeviceCMYK": 4}
)

// inlineImageBytes locates an inline image in a page's content: the BI that
// begins it, its data, and the EI that ends it.
type inlineImageBytes struct {
	// start and end bracket the image from its BI through its EI.
	start, end int
	// data is what lies between the white-space byte after ID and the EI.
	data []byte
	// keys are the dictionary's keys in the order the stream writes them
	// ([parser.Parser.InlineImageKeys]).
	keys []ast.PdfName
}

// blankInlineImage returns the edit that rewrites the inline image box names
// with every sample the zones cover cleared, as [blankImage] clears an image
// XObject: a picture's covered samples turn black in its own colour space, and
// a stencil's turn unpainted.
//
// An inline image has no object to replace: its dictionary and its data sit
// in the content stream between BI and EI (ISO 32000-1, 8.9.7), so the edit
// replaces those bytes with a new inline image. It stays inline rather than
// moving to an image XObject, which would add a /Resources entry to a page
// whose resources other pages may share, and the replacement keeps the
// colour space the original names, so no sample outside the zones changes
// value:
//
//   - a stencil mask comes back a stencil mask at one bit a sample;
//   - a picture in a device space, or one a /ColorSpace resource name maps to
//     a space [pdfimage.DecodeStreamNative] reads in its own components,
//     comes back under the same /CS at eight bits a sample;
//   - an indexed picture over a device base comes back in that base, each
//     index replaced by the colour its table gives it, which is the colour
//     ISO 32000-1, 8.6.6.3 paints for it.
//
// The data is written under FlateDecode, and under ASCIIHexDecode as well when
// the compressed bytes hold "EI", which a reader would take for the end of the
// data (ISO 32000-1, 8.9.7 gives the data no length that PDF 1.x readers
// honour).
//
// It refuses what it cannot rewrite exactly:
//
//   - [ErrRegionInFormXObject] for an inline image a form XObject draws,
//     whose bytes are in the form's stream;
//   - [ErrInlineImageInRegion] for an image whose bytes the readers do not
//     agree on, whose dictionary writes a key both abbreviated and in full or
//     holds a key Table 93 does not list, or whose colour space no inline
//     image can name once rewritten (any space other than those above);
//   - [ErrImageNotDecodable], [ErrImageCarriesSampleEntries] and
//     [ErrImageRegionCoversNoSample] as [blankImage] does.
//
// # Corpus evidence
//
//   - govdocs, pdfjs, pdfcpu: 544 pages of 73 files paint an inline image,
//     and every whole-page and centred-quarter redaction of them was refused
//     over it. Blanked in place, 252 of those 1088 redactions succeed, over
//     8842 inline images: each output passes qpdf --check, keeps no text in
//     the region, and holds no covered sample left uncleared. The rest now
//     stop at the refusal the inline image hid.
//   - pdfjs issue14256.pdf writes /W and /Width in one image and
//     issue10388_reduced.pdf draws its image through a form: the only two
//     files still refused over an inline image.
//   - govdocs, pdfjs: 2254 inline images name an indexed space over
//     DeviceRGB, 2253 of them through a /ColorSpace resource, and no other
//     indexed base occurs.
func blankInlineImage(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	data []byte,
	ops []content.Operation,
	ranges []content.ByteRange,
	ends *content.InlineImageEnds,
	box graphics.ImageBox,
	zones []heuristics.Zone,
) (SpanEdit, error) {
	// A form's image sits in the form's stream, and anything else the page's
	// operators run, such as a Type 3 glyph, records the index of the show
	// operator that ran it: an index that does not name a BI here is not this
	// image's.
	if box.FormDepth > 0 {
		return SpanEdit{}, fmt.Errorf("%w: an inline image", ErrRegionInFormXObject)
	}
	if box.Op < 0 || box.Op >= len(ops) || box.Op >= len(ranges) || ops[box.Op].Operator != "BI" || len(ops[box.Op].Operands) != 1 {
		return SpanEdit{}, fmt.Errorf("%w: an inline image no operation of the page's content stream draws", ErrRegionInFormXObject)
	}
	inline, ok := ops[box.Op].Operands[0].(ast.PdfDict)
	if !ok {
		return SpanEdit{}, fmt.Errorf("%w: operation %d holds no image dictionary", ErrInlineImageInRegion, box.Op)
	}

	loc, err := locateInlineImage(ctx, data, ranges[box.Op], inline, ends)
	if err != nil {
		return SpanEdit{}, fmt.Errorf("%w: operation %d: %w", ErrInlineImageInRegion, box.Op, err)
	}

	dict, err := inlineImageDict(ctx, alloc, doc, page, inline, loc.keys)
	if err != nil {
		return SpanEdit{}, fmt.Errorf("operation %d: %w", box.Op, err)
	}
	stream := ast.NewStream(dict, dctInlineData(dict, loc.data))

	text, err := blankInlineSamples(ctx, doc, page, inline, stream, box, zones)
	if err != nil {
		return SpanEdit{}, fmt.Errorf("inline image at operation %d: %w", box.Op, err)
	}
	return SpanEdit{Span: content.ByteRange{Start: loc.start, End: loc.end}, Replacement: text}, nil
}

// dctInlineData returns data, the data of an inline image whose dictionary
// dict applies DCTDecode first, without the white space before its JPEG SOI
// marker. ISO 32000-1, 8.9.7 has one white-space byte follow ID, which the
// data already starts past; a second, as a CR LF end of line gives, would be
// read as the start of the JPEG stream, which must open with SOI (ITU-T
// T.81, B.2.1). Deliberate leniency toward PDF.js, which skips the bytes
// before SOI (JpegStream, src/core/jpeg_stream.js) and so draws such an
// image, as every other filter's data is read from where the parse put it.
//
// # Corpus evidence
//
//   - pdfjs bug1065245.pdf: its DCT inline image follows ID with CR LF, and
//     the decode refused the data as missing its SOI marker.
func dctInlineData(dict ast.PdfDict, data []byte) []byte {
	filters, _ := dict["Filter"].(ast.PdfArray)
	if len(filters) == 0 || filters[0] != ast.PdfName("DCTDecode") {
		return data
	}
	trimmed := data
	for len(trimmed) > 0 && isContentWhiteSpace(trimmed[0]) {
		trimmed = trimmed[1:]
	}
	if len(trimmed) < 2 || trimmed[0] != 0xFF || trimmed[1] != 0xD8 {
		return data
	}
	return trimmed
}

// blankInlineSamples decodes the image stream describes, clears the samples
// the zones cover and returns the text of the inline image that replaces it.
// inline is the image's own dictionary, from which the replacement copies the
// colour space it names.
func blankInlineSamples(
	ctx context.Context,
	doc *document.Document,
	page *document.Page,
	inline ast.PdfDict,
	stream ast.PdfStream,
	box graphics.ImageBox,
	zones []heuristics.Zone,
) ([]byte, error) {
	// Resolved values only: inlineImageDict wrote no reference into the dict.
	mask, _ := stream.Dict["ImageMask"].(ast.PdfBool)
	isStencil := bool(mask)

	// An indexed image's /Decode remaps its indices, which the expansion
	// applies itself.
	if !isStencil && isIndexedSpace(stream.Dict["ColorSpace"]) {
		return blankInlineIndexed(ctx, doc, page, inline, stream, box, zones)
	}
	if _, present := stream.Dict["Decode"]; present && !pdfimage.AppliesDecodeArray(ctx, doc, stream.Dict) {
		return nil, fmt.Errorf("%w: it carries a /Decode the decode does not apply", ErrImageCarriesSampleEntries)
	}

	if !isStencil {
		native, ok, err := pdfimage.DecodeStreamNative(ctx, doc, page, graphics.InlineImageName, stream)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrImageNotDecodable, err)
		}
		if !ok {
			// The decode reads every other space into sRGB, which an inline
			// image can state only through a resource the page would have to
			// gain. Rewriting it in DeviceRGB would change the colour of
			// every sample outside the zones.
			return nil, fmt.Errorf("%w: its colour space cannot be written back inline", ErrInlineImageInRegion)
		}
		n := native.Components
		black, err := pdfimage.NativeBlack(ctx, doc, page, stream.Dict, n)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrImageNotDecodable, err)
		}
		if err := clearNativeSamples(box, zones, native.Data, native.Width, native.Height, black); err != nil {
			return nil, err
		}
		return inlinePictureText(ctx, inline, stream.Dict, native.Width, native.Height, inlineColorSpace(inline), native.Data)
	}

	img, err := pdfimage.DecodeStreamSamples(ctx, doc, page, graphics.InlineImageName, stream)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrImageNotDecodable, err)
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("%w: it has no samples", ErrImageNotDecodable)
	}
	canvas := image.NewNRGBA(bounds)
	draw.Draw(canvas, bounds, img, bounds.Min, draw.Src)
	blanked, err := blankCoveredSamples(canvas, box, zones, blankedStencil)
	if err != nil {
		return nil, err
	}
	if blanked == 0 {
		return nil, ErrImageRegionCoversNoSample
	}
	// No /D, for the reason packStencilBits states.
	return inlineImageText(ctx, ast.PdfDict{
		"IM":  ast.PdfBool(true),
		"W":   ast.PdfInt(w),
		"H":   ast.PdfInt(h),
		"BPC": ast.PdfInt(1),
	}, inline, stream.Dict, packStencilBits(canvas, w, h))
}

// clearNativeSamples sets every sample of a w x h image that the zones cover
// to black, the space's black [pdfimage.NativeBlack] gives, as [blankNative]
// does; the image has one component per byte of black.
func clearNativeSamples(box graphics.ImageBox, zones []heuristics.Zone, samples []byte, w, h int, black []byte) error {
	n := len(black)
	blanked, err := forCoveredSamples(box, zones, w, h, func(px, py int) {
		i := (py*w + px) * n
		copy(samples[i:i+n], black)
	})
	if err != nil {
		return err
	}
	if blanked == 0 {
		return ErrImageRegionCoversNoSample
	}
	return nil
}

// blankInlineIndexed clears the covered samples of an indexed inline image
// whose base is a device space, writing the image back in that base: each
// index is replaced by the base colour the table gives it (ISO 32000-1,
// 8.6.6.3), so every sample outside the zones paints what it painted, and a
// covered one is black. Keeping the indexed space would need an index that
// paints black, which the table need not hold.
//
// The indices are read as [pdfimage.DecodeStreamSamples] reads them: /Decode
// remaps the index range (8.9.5.2), an index past the table reads as the
// base's zero, and rows the data stops short of read as black.
func blankInlineIndexed(
	ctx context.Context,
	doc *document.Document,
	page *document.Page,
	inline ast.PdfDict,
	stream ast.PdfStream,
	box graphics.ImageBox,
	zones []heuristics.Zone,
) ([]byte, error) {
	cs, _ := stream.Dict["ColorSpace"].(ast.PdfArray)
	base, ok := cs[1].(ast.PdfName)
	n := deviceSpaceComponents[base]
	if !ok || n == 0 {
		return nil, fmt.Errorf("%w: its indexed base cannot be written back inline", ErrInlineImageInRegion)
	}
	hival, ok := document.Number(cs[2])
	if !ok || hival < 0 || hival > maxIndexedHival || hival != math.Trunc(hival) {
		return nil, fmt.Errorf("%w: its indexed colour space states hival %v", ErrImageNotDecodable, cs[2])
	}
	var table []byte
	switch v := cs[3].(type) {
	case ast.PdfString:
		table = []byte(v)
	case ast.PdfStream:
		decoded, err := doc.DecodeStream(ctx, v)
		if err != nil {
			return nil, fmt.Errorf("%w: its colour table: %w", ErrImageNotDecodable, err)
		}
		table = decoded
	default:
		return nil, fmt.Errorf("%w: its colour table is %T", ErrImageNotDecodable, cs[3])
	}
	if want := (int(hival) + 1) * n; len(table) > want {
		table = table[:want]
	}

	w, wOK := document.Number(stream.Dict["Width"])
	h, hOK := document.Number(stream.Dict["Height"])
	bpc, bOK := document.Number(stream.Dict["BitsPerComponent"])
	if !wOK || !hOK || !bOK || w < 1 || h < 1 || w != math.Trunc(w) || h != math.Trunc(h) ||
		w*h > float64(doc.Limits().MaxImagePixels) {
		return nil, fmt.Errorf("%w: it states %v x %v samples", ErrImageNotDecodable, stream.Dict["Width"], stream.Dict["Height"])
	}
	depth := int(bpc)
	if depth != 1 && depth != 2 && depth != 4 && depth != 8 {
		return nil, fmt.Errorf("%w: an indexed image at %v bits a sample", ErrImageNotDecodable, stream.Dict["BitsPerComponent"])
	}
	width, height := int(w), int(h)
	rowBytes := (width*depth + 7) / 8

	lut, err := indexDecodeTable(stream.Dict["Decode"], depth)
	if err != nil {
		return nil, err
	}

	rc, err := doc.NewStreamReaderChecked(ctx, stream)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrImageNotDecodable, err)
	}
	indices, err := io.ReadAll(io.LimitReader(rc, int64(rowBytes)*int64(height)))
	if closeErr := rc.Close(); closeErr != nil {
		doc.Logger.Debug("closing inline image reader", slog.Any("err", closeErr))
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrImageNotDecodable, err)
	}

	// The base is written back as the device space it names, which a
	// default colour space of the page stands in for (ISO 32000-1, 8.6.5.6),
	// so its black is read as the replacement paints.
	black, err := pdfimage.NativeBlack(ctx, doc, page, ast.PdfDict{"ColorSpace": base}, n)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrImageNotDecodable, err)
	}
	samples := make([]byte, width*height*n)
	maxVal := byte(1<<depth - 1)
	for y := range height {
		row := y * rowBytes
		for x := range width {
			out := samples[(y*width+x)*n:][:n]
			if row+rowBytes > len(indices) {
				copy(out, black)
				continue
			}
			index := int(lut[filter.BitSample(indices[row:row+rowBytes], x, depth, maxVal)])
			if entry := index * n; entry+n <= len(table) {
				copy(out, table[entry:entry+n])
			}
		}
	}
	if err := clearNativeSamples(box, zones, samples, width, height, black); err != nil {
		return nil, err
	}
	return inlinePictureText(ctx, inline, stream.Dict, width, height, base, samples)
}

// indexDecodeTable returns the index each sample value of an indexed image
// at depth bits a sample reads as under its /Decode array, which maps the
// range 0 to 2^depth - 1 onto [Dmin Dmax] (ISO 32000-1, 8.9.5.2). An absent
// array maps each value to itself.
func indexDecodeTable(decode ast.PdfObject, depth int) ([256]byte, error) {
	var lut [256]byte
	top := float64(int(1)<<depth - 1)
	lo, hi := 0.0, top
	if decode != nil {
		arr, ok := decode.(ast.PdfArray)
		if !ok || len(arr) != 2 {
			return lut, fmt.Errorf("%w: its /Decode is no pair of numbers", ErrImageCarriesSampleEntries)
		}
		var lok, hok bool
		lo, lok = document.Number(arr[0])
		hi, hok = document.Number(arr[1])
		if !lok || !hok {
			return lut, fmt.Errorf("%w: its /Decode is no pair of numbers", ErrImageCarriesSampleEntries)
		}
	}
	for s := range lut {
		v := lo + float64(s)*(hi-lo)/top
		lut[s] = byte(min(max(math.Round(v), 0), 255))
	}
	return lut, nil
}

// inlinePictureText returns the text of the inline image that paints samples,
// w x h pixels at eight bits a component in the colour space cs names, with
// the rendering keys of the image it replaces.
func inlinePictureText(ctx context.Context, inline, dict ast.PdfDict, w, h int, cs ast.PdfObject, samples []byte) ([]byte, error) {
	return inlineImageText(ctx, ast.PdfDict{
		"W":   ast.PdfInt(w),
		"H":   ast.PdfInt(h),
		"CS":  cs,
		"BPC": ast.PdfInt(8),
	}, inline, dict, samples)
}

// inlineImageText returns the bytes of an inline image whose dictionary holds
// head, the /Interpolate and /Intent of dict, which describe how the image
// renders rather than its samples, and the filters its data is written under,
// and whose data is raw compressed. inline is the dictionary of the image it
// replaces, whose spelling of /Interpolate it keeps.
func inlineImageText(ctx context.Context, head, inline, dict ast.PdfDict, raw []byte) ([]byte, error) {
	compressed, err := flateCompress(raw)
	if err != nil {
		return nil, fmt.Errorf("compressing the blanked inline image: %w", err)
	}
	filters := ast.PdfObject(ast.PdfName("Fl"))
	if bytes.Contains(compressed, []byte("EI")) {
		encoded := make([]byte, hex.EncodedLen(len(compressed))+1)
		hex.Encode(encoded, compressed)
		encoded[len(encoded)-1] = '>'
		compressed = bytes.ToUpper(encoded)
		filters = ast.PdfArray{ast.PdfName("AHx"), ast.PdfName("Fl")}
	}
	head["F"] = filters
	if v, ok := dict["Interpolate"]; ok {
		key := ast.PdfName("I")
		if _, full := inline["Interpolate"]; full {
			key = "Interpolate"
		}
		head[key] = v
	}
	if v, ok := dict["Intent"]; ok {
		head["Intent"] = v
	}

	var text bytes.Buffer
	text.WriteString("BI")
	// The order is fixed so the same input writes the same bytes.
	for _, key := range []ast.PdfName{"IM", "W", "H", "CS", "BPC", "F", "I", "Interpolate", "Intent"} {
		v, ok := head[key]
		if !ok {
			continue
		}
		text.WriteByte(' ')
		if _, err := writer.SerializeName(&text, key); err != nil {
			return nil, fmt.Errorf("writing the blanked inline image: %w", err)
		}
		text.WriteByte(' ')
		if _, err := writer.Serialize(ctx, &text, v); err != nil {
			return nil, fmt.Errorf("writing the blanked inline image: %w", err)
		}
	}
	// One white-space byte after ID begins the data, and one before EI is the
	// separator readers look for (ISO 32000-1, 8.9.7).
	text.WriteString(" ID ")
	text.Write(compressed)
	text.WriteString("\nEI")
	return text.Bytes(), nil
}

// inlineColorSpace returns the /CS value of an inline image dictionary as the
// image wrote it, abbreviated or in full, so the replacement names the same
// space through the same resources.
func inlineColorSpace(inline ast.PdfDict) ast.PdfObject {
	if v, ok := inline["CS"]; ok {
		return v
	}
	return inline["ColorSpace"]
}

// isIndexedSpace reports whether cs, a resolved colour space, is indexed.
func isIndexedSpace(cs ast.PdfObject) bool {
	arr, ok := cs.(ast.PdfArray)
	if !ok || len(arr) != 4 {
		return false
	}
	family, _ := arr[0].(ast.PdfName)
	return family == "Indexed"
}

// inlineImageDict returns the image dictionary of ISO 32000-1, 8.9.5, Table 89
// the inline image dictionary inline abbreviates (8.9.7, Table 93), with its
// filters spelled in full and its colour space resolved: a device space by its
// full name, a name through the page's /ColorSpace resources, and an Indexed
// array, abbreviated or not, over either. The result names no object, so the
// image decoders read it as they read an image XObject's.
//
// order lists the keys of inline in the order the stream writes them. The
// entries are read in that order, and any key it leaves out after them in
// sorted order, so a refusal names the first offending entry the stream
// writes, the same one every run.
func inlineImageDict(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	inline ast.PdfDict,
	order []ast.PdfName,
) (ast.PdfDict, error) {
	dict := ast.PdfDict{"Type": ast.PdfName("XObject"), "Subtype": ast.PdfName("Image")}
	for _, key := range inlineKeyOrder(inline, order) {
		v := inline[key]
		full, known := inlineImageKeys[key]
		if !known {
			// A key Table 93 does not list may mean something to a reader
			// that this rewrite would drop.
			return nil, fmt.Errorf("%w: its dictionary holds %s", ErrInlineImageInRegion, key)
		}
		if _, both := dict[full]; both {
			// A reader may take either spelling, so no rewrite of one is
			// sure to keep what it painted.
			return nil, fmt.Errorf("%w: its dictionary writes %s twice", ErrInlineImageInRegion, full)
		}
		resolved, err := resolveAllocFirst(ctx, alloc, doc, v)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrImageNotDecodable, key, err)
		}
		dict[full] = resolved
	}
	delete(dict, "Length")

	if f, ok := dict["Filter"]; ok {
		filters, err := inlineFilterNames(f)
		if err != nil {
			return nil, err
		}
		dict["Filter"] = filters
	}
	if cs, ok := dict["ColorSpace"]; ok {
		resolved, err := inlineImageColorSpace(ctx, alloc, doc, page, cs, true)
		if err != nil {
			return nil, err
		}
		dict["ColorSpace"] = resolved
	}
	return dict, nil
}

// inlineKeyOrder returns the keys of inline, each once: those order names, in
// its order, then the rest sorted.
func inlineKeyOrder(inline ast.PdfDict, order []ast.PdfName) []ast.PdfName {
	keys := make([]ast.PdfName, 0, len(inline))
	seen := make(map[ast.PdfName]bool, len(inline))
	for _, k := range order {
		if _, ok := inline[k]; ok && !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	rest := make([]ast.PdfName, 0, len(inline)-len(keys))
	for k := range inline {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	slices.Sort(rest)
	return append(keys, rest...)
}

// inlineFilterNames returns the filters f names, a name or an array of names,
// spelled in full. A name neither Table 6 nor Table 94 of ISO 32000-1 lists is
// one the decoder cannot read.
func inlineFilterNames(f ast.PdfObject) (ast.PdfArray, error) {
	var names ast.PdfArray
	switch v := f.(type) {
	case ast.PdfName:
		names = ast.PdfArray{v}
	case ast.PdfArray:
		names = v
	default:
		return nil, fmt.Errorf("%w: its /Filter is %T", ErrImageNotDecodable, f)
	}
	out := make(ast.PdfArray, len(names))
	for i, n := range names {
		name, _ := n.(ast.PdfName)
		full, ok := inlineFilters[name]
		if !ok {
			return nil, fmt.Errorf("%w: it applies %v", ErrImageNotDecodable, n)
		}
		out[i] = full
	}
	return out, nil
}

// inlineImageColorSpace resolves the colour space v an inline image names
// (ISO 32000-1, 8.9.7): a device space, abbreviated or in full, a name the
// page's /ColorSpace resources hold, or, where indexed allows, an Indexed
// array, /I or /Indexed, over either. Indexed takes no Indexed base
// (8.6.6.3).
func inlineImageColorSpace(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	v ast.PdfObject,
	indexed bool,
) (ast.PdfObject, error) {
	switch cs := v.(type) {
	case ast.PdfName:
		if full, ok := inlineDeviceSpaces[cs]; ok {
			return full, nil
		}
		resolved, err := pageColorSpace(ctx, alloc, doc, page, cs)
		if err != nil {
			return nil, err
		}
		if arr, ok := resolved.(ast.PdfArray); ok && indexed {
			return resolvedIndexed(ctx, alloc, doc, page, arr)
		}
		return resolved, nil
	case ast.PdfArray:
		if !indexed {
			return nil, fmt.Errorf("%w: an indexed base that is %v", ErrImageNotDecodable, cs)
		}
		return resolvedIndexed(ctx, alloc, doc, page, cs)
	}
	return nil, fmt.Errorf("%w: its colour space is %T", ErrImageNotDecodable, v)
}

// resolvedIndexed returns arr with its base resolved when it is an Indexed
// colour space, abbreviated /I or in full, and arr as it is otherwise.
func resolvedIndexed(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	arr ast.PdfArray,
) (ast.PdfObject, error) {
	if len(arr) == 0 {
		return arr, nil
	}
	family, err := resolveAllocFirst(ctx, alloc, doc, arr[0])
	if err != nil {
		return nil, fmt.Errorf("%w: its colour space: %w", ErrImageNotDecodable, err)
	}
	if family != ast.PdfName("I") && family != ast.PdfName("Indexed") {
		return arr, nil
	}
	if len(arr) != 4 {
		return nil, fmt.Errorf("%w: an indexed colour space of %d entries", ErrImageNotDecodable, len(arr))
	}
	out := make(ast.PdfArray, 4)
	out[0] = ast.PdfName("Indexed")
	for i := 1; i < 4; i++ {
		resolved, err := resolveAllocFirst(ctx, alloc, doc, arr[i])
		if err != nil {
			return nil, fmt.Errorf("%w: its indexed colour space: %w", ErrImageNotDecodable, err)
		}
		out[i] = resolved
	}
	base, err := inlineImageColorSpace(ctx, alloc, doc, page, out[1], false)
	if err != nil {
		return nil, err
	}
	out[1] = base
	return out, nil
}

// pageColorSpace returns the colour space the page's /ColorSpace resources
// hold under name, read as earlier stages staged it.
func pageColorSpace(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	name ast.PdfName,
) (ast.PdfObject, error) {
	spaces, err := resolveDictAllocFirst(ctx, alloc, doc, page.Resources["ColorSpace"])
	if err != nil || spaces == nil {
		return nil, fmt.Errorf("%w: no /ColorSpace resource names %s", ErrImageNotDecodable, name)
	}
	entry, ok := spaces[name]
	if !ok {
		return nil, fmt.Errorf("%w: no /ColorSpace resource names %s", ErrImageNotDecodable, name)
	}
	resolved, err := resolveAllocFirst(ctx, alloc, doc, entry)
	if err != nil {
		return nil, fmt.Errorf("%w: colour space %s: %w", ErrImageNotDecodable, name, err)
	}
	return resolved, nil
}

// errInlineBytes reports an inline image whose bytes the content does not
// hold as the parse read them.
var errInlineBytes = errors.New("its bytes do not read back as the parse read them")

// locateInlineImage finds, in the content data, the inline image of the BI
// operation spanning r whose dictionary is dict.
//
// The parse ends the data as PDF.js does, and ISO 32000-1, 8.9.7 gives it no
// length a PDF 1.x reader honours, so readers can disagree on where it ends.
// A reader that reads past that end takes bytes the parse read as operators
// for samples, which no rewrite of the parsed span clears, so an image whose
// data veraPDF ends elsewhere is refused (see [content.InlineImageEnds]).
func locateInlineImage(ctx context.Context, data []byte, r content.ByteRange, dict ast.PdfDict, ends *content.InlineImageEnds) (inlineImageBytes, error) {
	if r.Start < 0 || r.Start > r.End || r.End > len(data) {
		return inlineImageBytes{}, errInlineBytes
	}
	start, ok := inlineImageKeyword(data[r.Start:r.End])
	if !ok {
		return inlineImageBytes{}, errInlineBytes
	}
	start += r.Start

	p := parser.NewParser(lexer.NewLexer(ioat.NewReader(bytes.NewReader(data[start:r.End]), int64(r.End-start), 0))).WithOffsetTracking()
	if _, err := p.Consume(); err != nil {
		return inlineImageBytes{}, errInlineBytes
	}
	got, err := p.ParseInlineImageHeader(ctx)
	if err != nil || !reflect.DeepEqual(got, dict) {
		return inlineImageBytes{}, errInlineBytes
	}
	idEnd := start + int(p.Position())
	keys := p.InlineImageKeys()

	if other := ends.End(ctx, idEnd, dict); other != r.End {
		return inlineImageBytes{}, fmt.Errorf("readers end its data at offsets %d and %d", r.End, other)
	}

	end := r.End
	for end > idEnd && isContentWhiteSpace(data[end-1]) {
		end--
	}
	if end-idEnd < len("EI") || string(data[end-len("EI"):end]) != "EI" {
		return inlineImageBytes{}, errInlineBytes
	}
	dataStart := idEnd
	if dataStart < end-len("EI") && isContentWhiteSpace(data[dataStart]) {
		dataStart++
	}
	return inlineImageBytes{start: start, end: end, data: data[dataStart : end-len("EI")], keys: keys}, nil
}

// inlineImageKeyword returns the offset in span, the bytes of a BI
// operation, of the BI keyword that begins its inline image: the first BI
// keyword outside every array and dictionary.
func inlineImageKeyword(span []byte) (int, bool) {
	p := parser.NewParser(lexer.NewLexer(ioat.NewReader(bytes.NewReader(span), int64(len(span)), 0))).WithOffsetTracking()
	depth := 0
	for {
		tok, err := p.Peek(0)
		if err != nil || tok.Type == lexer.TokenError || tok.Type == lexer.TokenEOF {
			return 0, false
		}
		end := int(p.PeekEnd(0))
		if _, err := p.Consume(); err != nil {
			return 0, false
		}
		switch {
		case tok.Type == lexer.TokenDelimiter && (tok.Value == "[" || tok.Value == "<<"):
			depth++
		case tok.Type == lexer.TokenDelimiter && (tok.Value == "]" || tok.Value == ">>"):
			depth = max(depth-1, 0)
		case tok.Type == lexer.TokenKeyword && tok.Value == "BI" && depth == 0:
			return end - len("BI"), true
		}
	}
}

// isContentWhiteSpace reports whether c is white space (ISO 32000-1, 7.2.2,
// Table 1).
func isContentWhiteSpace(c byte) bool {
	switch c {
	case 0, '\t', '\n', '\f', '\r', ' ':
		return true
	}
	return false
}
