package writer

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"maps"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/filter/dct"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// ImageRecompression configures [RecompressImages]: the decoder that reads the
// images' current encoding, and the budgets that bound the pass.
type ImageRecompression struct {
	// Decoder decodes the images' current filter chains. Its plugins are
	// consulted only to leave alone an image whose filter one claims, so a nil
	// Decoder decodes with the built-in filters under the default budgets.
	Decoder *filter.Decoder
	// Limits bounds the pass: an image past MaxImagePixels is left as it is,
	// and MaxStreamBytes bounds both what one decode holds and how many
	// re-encoded bytes the pass keeps in memory (see [RecompressImages]).
	Limits limits.Limits
}

// RecompressStats reports what [RecompressImages] changed.
type RecompressStats struct {
	// Images is the number of image streams re-encoded.
	Images int
	// SavedBytes is how many bytes the re-encoded objects are smaller by, as
	// serialized.
	SavedBytes int64
}

// RecompressImages re-encodes, losslessly, the image XObjects of the allocator
// whose filters litho can both decode and replace, keeping a re-encoding only
// when it serializes smaller than the stream it replaces.
//
// An image qualifies when its filter chain holds nothing but FlateDecode,
// LZWDecode, RunLengthDecode, ASCIIHexDecode and ASCII85Decode, or no filter at
// all. Each is a general-purpose lossless filter (ISO 32000-1, 7.4.2 to 7.4.5),
// so the decoded bytes are the samples and nothing a viewer reads depends on
// how they were compressed. The candidates are FlateDecode without a
// predictor and with a PNG predictor (ISO 32000-1, 7.4.4.4): the smaller at
// zlib's default level is compressed again at its best level, and kept if the
// result is smaller than the stream it replaces.
//
// An image whose only filter is DCTDecode keeps it: its JPEG data is re-coded
// with Huffman tables built from its own statistics (ITU-T T.81, Annex K.2)
// and without restart intervals, which codes the same quantized coefficients
// in fewer bytes, so every
// decoder reconstructs the same samples ([dct.OptimizeHuffman]). Only
// sequential Huffman-coded JPEG qualifies; progressive data, and data that
// does not decode exactly to its markers, is left as it is. JPXDecode,
// JBIG2Decode and CCITTFaxDecode images, and JPEG data under another filter,
// are left alone: each is a format of its own, and a Flate re-encoding would
// replace it with the decoded samples, a different trade rather than a
// lossless improvement of the same one.
//
// An 8-bit RGB image of at most 256 colours is also weighed as indices into
// an Indexed colour space over its own base, which stores a pixel in 1, 2, 4
// or 8 bits rather than 24 ([indexedForm]), and that form is kept when it
// serializes smaller than both the stream it replaces and the image's own
// samples re-encoded.
//
// For the general-purpose filters only /Filter, /DecodeParms and the data
// change, and for a JPEG only the data. An Indexed form also changes
// /ColorSpace and /BitsPerComponent, drops a default /Decode, and renumbers a
// colour-key /Mask; every pixel keeps its colour. /SMask, /Width, /Height and
// /DL keep their values, and every re-encoding is decoded again and compared
// with the samples it encodes before it replaces them, so a stream whose
// pixels would differ is never written.
//
// An image is left as it is, without failing the write, when its dictionary
// names external data (/F), its filter keys are neither direct nor one
// reference away, a filter plugin claims one of its filters (litho cannot
// vouch for what a plugin decodes), its samples exceed the image budget, or
// decoding meets damaged or salvaged data: re-encoding what litho salvaged
// would hand every reader litho's reading of a damaged stream as if it were
// the file's.
//
// Re-encoded bytes are kept in memory up to the stream budget in total, a
// cache of what that budget bounds, which derives from it as
// [limits.Limits.MaxObjStmCacheBytes] does. Past it, an image's re-encoding is
// redone from its source when the stream is serialized, so beyond that budget
// the pass holds one image's samples at a time, as the resource-streaming rule
// asks. Images are visited in object-number order, and zlib output and a
// JPEG's re-coding depend on their input alone, so the result does too.
//
// The caller must not run this on an encrypted document: encryption has
// already replaced every stream's data with ciphertext by the time a rewrite
// reaches this point. [WriteRewrite] declines it there, as it declines the
// other compaction passes.
func RecompressImages(ctx context.Context, alloc *ObjAllocator, cfg ImageRecompression) (RecompressStats, error) {
	lim := cfg.Limits.WithDefaults()
	pending := alloc.Pending()

	nums := make([]int, 0, len(pending))
	for num, obj := range pending {
		if s, ok := obj.(ast.PdfStream); ok && isImageStream(s.Dict) {
			nums = append(nums, num)
		}
	}
	sort.Ints(nums)

	var (
		stats    RecompressStats
		retained int64
	)
	for _, num := range nums {
		if err := ctx.Err(); err != nil {
			return stats, fmt.Errorf("recompressing images: %w", err)
		}
		s, ok := pending[num].(ast.PdfStream)
		if !ok {
			continue
		}
		plan, ok, err := planRecompression(ctx, pending, s, cfg.Decoder, lim)
		if err != nil {
			return stats, fmt.Errorf("recompressing image %d: %w", num, err)
		}
		if !ok {
			continue
		}
		var replacement ast.PdfStream
		if size := int64(len(plan.encoded)); retained+size <= lim.MaxStreamBytes {
			retained += size
			replacement = ast.NewStream(plan.dict, plan.encoded)
		} else {
			replacement = ast.NewLazyStream(plan.dict, plan.reencoder(s.Bytes, cfg.Decoder))
		}
		alloc.AllocAt(num, replacement)
		stats.Images++
		stats.SavedBytes += plan.saved
	}
	return stats, nil
}

// isImageStream reports whether dict is an image XObject's (ISO 32000-1,
// Table 89). Thumbnails carry no /Subtype and are left out, which costs only
// their share of the saving.
func isImageStream(dict ast.PdfDict) bool {
	subtype, _ := dict["Subtype"].(ast.PdfName)
	return subtype == "Image"
}

// recompressibleFilters are the filters an image's chain may hold for
// [RecompressImages] to re-encode it: the general-purpose lossless ones of
// ISO 32000-1, 7.4.2 to 7.4.5, whose output is the samples themselves.
var recompressibleFilters = map[ast.PdfName]bool{
	"FlateDecode": true, "Fl": true,
	"LZWDecode": true, "LZW": true,
	"RunLengthDecode": true, "RL": true,
	"ASCIIHexDecode": true, "AHx": true,
	"ASCII85Decode": true, "A85": true,
}

// screenSlack bounds how far the screening encoding of [planRecompression]
// may exceed the stream it would replace for the best level to be tried at
// all: an image whose default-level encoding is more than an eighth larger is
// left alone without paying for the best level. Not a resource cap: it only
// skips work whose saving would be implausible, since zlib's best level
// rarely beats its default by that much on data the default already handles,
// and keeping such an image unchanged is always correct.
const screenSlack = 8

// recompressionPlan is the re-encoding [planRecompression] chose for one image.
type recompressionPlan struct {
	// source is the image's dictionary with its filter keys made direct, the
	// view the decoder reads its current encoding through.
	source  ast.PdfDict
	dict    ast.PdfDict
	encoded []byte
	saved   int64
	layout  *predictorLayout
	// jpeg marks a DCTDecode image whose JPEG data was re-coded with
	// optimized Huffman tables (see [planJPEG]) rather than compressed with
	// FlateDecode.
	jpeg bool
	// indexed is the palette of an image rewritten as indices into an
	// Indexed colour space (see [indexedForm]), nil when its samples are
	// written as they decode.
	indexed *indexedPalette
}

// predictorLayout is the sample layout a PNG predictor is applied under.
type predictorLayout struct {
	colors, bpc, columns int
}

// planRecompression decodes s and returns its best lossless re-encoding, or
// false when s does not qualify or no re-encoding is smaller. The error is
// only ever the context's: every other failure leaves the image as it is.
func planRecompression(
	ctx context.Context,
	pending map[int]ast.PdfObject,
	s ast.PdfStream,
	dec *filter.Decoder,
	lim limits.Limits,
) (recompressionPlan, bool, error) {
	if _, external := s.Dict["F"]; external {
		return recompressionPlan{}, false, nil
	}
	source, ok := DirectFilterKeys(pending, s.Dict)
	if !ok {
		return recompressionPlan{}, false, nil
	}
	jpeg := chainIsDCT(source["Filter"], dec)
	if !jpeg && !chainRecompressible(source["Filter"], dec) {
		return recompressionPlan{}, false, nil
	}

	width, okW := pendingInt(pending, s.Dict["Width"])
	height, okH := pendingInt(pending, s.Dict["Height"])
	if !okW || !okH || width < 1 || height < 1 || width > lim.MaxImagePixels/height {
		return recompressionPlan{}, false, nil
	}
	if jpeg {
		return planJPEG(ctx, s, source, dec)
	}

	decoded, ok, err := DecodeWhole(ctx, dec, ast.NewLazyStream(source, s.Bytes))
	if err != nil || !ok {
		return recompressionPlan{}, false, err
	}

	before, err := serializedSize(ctx, s)
	if err != nil {
		return recompressionPlan{}, false, ctx.Err()
	}

	forms := []sampleForm{{
		samples: decoded,
		dict:    s.Dict,
		layout:  samplePredictorLayout(pending, s.Dict, width, height, len(decoded)),
	}}
	if f, ok := indexedForm(pending, s.Dict, decoded, width, height); ok {
		forms = append(forms, f)
	}
	// Within a form the layout is chosen at zlib's default level, which ranks
	// a form's layouts as the best level does at a fraction of its cost. Each
	// form's winner is then compressed at the best level and the smallest
	// kept: across forms the default level ranks less reliably, since indices
	// and the samples they stand for compress differently.
	var (
		form    sampleForm
		encoded []byte
		dict    ast.PdfDict
		after   int64 = -1
	)
	for _, f := range forms {
		layout, screened, err := screenLayouts(ctx, f)
		if err != nil {
			return recompressionPlan{}, false, err
		}
		if screened < 0 || int64(screened) > before+before/screenSlack {
			continue
		}
		if err := ctx.Err(); err != nil {
			return recompressionPlan{}, false, err
		}
		enc, d, ok := encodeCandidate(f.samples, f.dict, layout, zlib.BestCompression)
		if !ok {
			continue
		}
		size, err := serializedSize(ctx, ast.NewStream(d, enc))
		if err != nil {
			// The source serialized, and the replacement differs from it only
			// in entries litho wrote, so only the context can fail it.
			return recompressionPlan{}, false, ctx.Err()
		}
		if size < before && (after < 0 || size < after) {
			form, encoded, dict, after = f, enc, d, size
			form.layout = layout
		}
	}
	if after < 0 {
		return recompressionPlan{}, false, nil
	}
	best := recompressionPlan{
		source: source, dict: dict, encoded: encoded, saved: before - after,
		layout: form.layout, indexed: form.indexed,
	}

	// The re-encoding replaces the samples only if it decodes to them: to the
	// source's samples, or for an Indexed form to the indices [indexedForm]
	// proved map back to them.
	again, ok, err := DecodeWhole(ctx, dec, ast.NewStream(best.dict, best.encoded))
	if err != nil || !ok || !bytes.Equal(again, form.samples) {
		return recompressionPlan{}, false, err
	}
	return best, true, nil
}

// screenLayouts returns the layout under which f's samples compress smallest
// at zlib's default level, nil for no predictor, and that size, or -1 when no
// layout encodes. The error is only ever the context's.
func screenLayouts(ctx context.Context, f sampleForm) (*predictorLayout, int, error) {
	layouts := []*predictorLayout{nil}
	if f.layout != nil {
		layouts = append(layouts, f.layout)
	}
	var (
		best     *predictorLayout
		screened = -1
	)
	for _, l := range layouts {
		if err := ctx.Err(); err != nil {
			return nil, -1, err
		}
		encoded, _, ok := encodeCandidate(f.samples, f.dict, l, zlib.DefaultCompression)
		if ok && (screened < 0 || len(encoded) < screened) {
			best, screened = l, len(encoded)
		}
	}
	return best, screened, nil
}

// planJPEG re-codes the JPEG data of a DCTDecode image with Huffman tables
// optimized for it ([dct.OptimizeHuffman]), and returns the plan when the
// stream serializes smaller. source is the image's dictionary with its filter
// keys made direct. The dictionary keeps every entry, /DecodeParms and its
// /ColorTransform included: only the entropy coding changes, and the
// quantized coefficients it codes do not (ISO 32000-1, 7.4.8; ITU-T T.81,
// Annex K.2). The
// re-coding is decoded again and compared with the source's samples before it
// replaces them, as every other re-encoding is.
func planJPEG(ctx context.Context, s ast.PdfStream, source ast.PdfDict, dec *filter.Decoder) (recompressionPlan, bool, error) {
	raw, err := s.Bytes(ctx)
	if err != nil {
		return recompressionPlan{}, false, ctx.Err()
	}
	encoded, ok, err := dct.OptimizeHuffman(ctx, raw)
	if err != nil || !ok {
		return recompressionPlan{}, false, err
	}
	before, err := serializedSize(ctx, s)
	if err != nil {
		return recompressionPlan{}, false, ctx.Err()
	}
	after, err := serializedSize(ctx, ast.NewStream(s.Dict, encoded))
	if err != nil {
		return recompressionPlan{}, false, ctx.Err()
	}
	if after >= before {
		return recompressionPlan{}, false, nil
	}

	decoded, ok, err := DecodeWhole(ctx, dec, ast.NewStream(source, raw))
	if err != nil || !ok {
		return recompressionPlan{}, false, err
	}
	again, ok, err := DecodeWhole(ctx, dec, ast.NewStream(source, encoded))
	if err != nil || !ok || !bytes.Equal(again, decoded) {
		return recompressionPlan{}, false, err
	}
	return recompressionPlan{source: source, dict: s.Dict, encoded: encoded, saved: before - after, jpeg: true}, true, nil
}

// chainIsDCT reports whether a /Filter value, already direct, is DCTDecode
// alone, as a name or a one-element array, and no plugin of dec claims it.
// A chain that wraps the JPEG data in another filter is left to
// [chainRecompressible], which declines it.
func chainIsDCT(f ast.PdfObject, dec *filter.Decoder) bool {
	name, ok := f.(ast.PdfName)
	if arr, isArr := f.(ast.PdfArray); isArr && len(arr) == 1 {
		name, ok = arr[0].(ast.PdfName)
	}
	return ok && (name == "DCTDecode" || name == "DCT") && !dec.Handles(name)
}

// reencoder returns a loader that redoes the plan's re-encoding from the
// image's source bytes, for an image whose re-encoding [RecompressImages] did
// not keep in memory. The result must have the length the plan measured,
// since the pass chose it on that size: any other length means the source
// changed under it.
func (p recompressionPlan) reencoder(
	raw func(context.Context) ([]byte, error),
	dec *filter.Decoder,
) func(context.Context) ([]byte, error) {
	source, dict, layout, palette, want := p.source, p.dict, p.layout, p.indexed, len(p.encoded)
	if p.jpeg {
		return func(ctx context.Context) ([]byte, error) {
			data, err := raw(ctx)
			if err != nil {
				return nil, fmt.Errorf("reading a recompressed image's source: %w", err)
			}
			encoded, ok, err := dct.OptimizeHuffman(ctx, data)
			if err != nil {
				return nil, fmt.Errorf("re-coding a recompressed image's source: %w", err)
			}
			if !ok || len(encoded) != want {
				return nil, fmt.Errorf("%w: %d bytes where %d were measured", ErrRecompressionDiverged, len(encoded), want)
			}
			return encoded, nil
		}
	}
	return func(ctx context.Context) ([]byte, error) {
		decoded, ok, err := DecodeWhole(ctx, dec, ast.NewLazyStream(source, raw))
		if err != nil {
			return nil, fmt.Errorf("decoding a recompressed image's source: %w", err)
		}
		if !ok {
			return nil, fmt.Errorf("%w: its source no longer decodes whole", ErrRecompressionDiverged)
		}
		if palette != nil {
			if decoded, ok = palette.indices(decoded); !ok {
				return nil, fmt.Errorf("%w: a colour of its source is not in its palette", ErrRecompressionDiverged)
			}
		}
		encoded, _, ok := encodeCandidate(decoded, dict, layout, zlib.BestCompression)
		if !ok || len(encoded) != want {
			return nil, fmt.Errorf("%w: %d bytes where %d were measured", ErrRecompressionDiverged, len(encoded), want)
		}
		return encoded, nil
	}
}

// DecodeWhole decodes s through its filter chain, reporting false when a
// filter was skipped or met damaged data, or when decoding failed for any
// reason but the context's. The decoder refuses salvaged data
// ([filter.Decoder.RefuseSalvaged]) so a stream litho reads leniently is
// never rewritten as though that reading were the file's.
func DecodeWhole(ctx context.Context, dec *filter.Decoder, s ast.PdfStream) ([]byte, bool, error) {
	data, report, err := dec.RefuseSalvaged().DecodeStreamReport(ctx, s)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, false, ctxErr
	}
	if err != nil || report.Incomplete() || len(report.Salvaged) > 0 {
		return nil, false, nil //nolint:nilerr // A stream that does not decode whole is left as it is, which is not a failed write.
	}
	return data, true, nil
}

// chainRecompressible reports whether a /Filter value, already direct, names
// only [recompressibleFilters] that no plugin of dec claims. An absent or null
// /Filter is an unencoded stream, which qualifies.
func chainRecompressible(f ast.PdfObject, dec *filter.Decoder) bool {
	var names []ast.PdfName
	switch v := f.(type) {
	case nil, ast.PdfNull:
		return true
	case ast.PdfName:
		names = []ast.PdfName{v}
	case ast.PdfArray:
		for _, elem := range v {
			name, ok := elem.(ast.PdfName)
			if !ok {
				return false
			}
			names = append(names, name)
		}
	default:
		return false
	}
	for _, name := range names {
		if !recompressibleFilters[name] || dec.Handles(name) {
			return false
		}
	}
	return true
}

// DirectFilterKeys returns dict with /Filter and /DecodeParms, and the
// elements of either when it is an array, resolved to direct values through
// pending, or false when one of them is neither direct nor a reference to a
// direct value. The decoder reads only direct filter keys; the source's own
// resolution of indirect ones went through object numbers this rewrite
// renumbered, so the pending objects are what they now name.
func DirectFilterKeys(pending map[int]ast.PdfObject, dict ast.PdfDict) (ast.PdfDict, bool) {
	out, cloned := dict, false
	for _, key := range []ast.PdfName{"Filter", "DecodeParms"} {
		v, present := dict[key]
		if !present {
			continue
		}
		resolved, changed, ok := followPending(pending, v)
		if !ok {
			return nil, false
		}
		if arr, isArr := resolved.(ast.PdfArray); isArr {
			var elems ast.PdfArray
			for i, elem := range arr {
				r, elemChanged, ok := followPending(pending, elem)
				if !ok {
					return nil, false
				}
				if elemChanged && elems == nil {
					elems = make(ast.PdfArray, len(arr))
					copy(elems, arr)
				}
				if elems != nil {
					elems[i] = r
				}
			}
			if elems != nil {
				resolved, changed = elems, true
			}
		}
		if !changed {
			continue
		}
		if !cloned {
			out, cloned = maps.Clone(dict), true
		}
		out[key] = resolved
	}
	return out, true
}

// followPending returns obj, or the pending object it references, reporting
// whether it followed a reference, and false when the reference names nothing
// pending or names another reference.
func followPending(pending map[int]ast.PdfObject, obj ast.PdfObject) (ast.PdfObject, bool, bool) {
	ref, isRef := obj.(ast.PdfIndirectRef)
	if !isRef {
		return obj, false, true
	}
	target, ok := pending[ref.ObjectNum]
	if !ok {
		return nil, false, false
	}
	switch target.(type) {
	case ast.PdfIndirectRef, ast.PdfStream:
		return nil, false, false
	}
	return target, true, true
}

// pendingInt returns obj as an integer, through one reference into pending.
func pendingInt(pending map[int]ast.PdfObject, obj ast.PdfObject) (int64, bool) {
	resolved, _, ok := followPending(pending, obj)
	if !ok {
		return 0, false
	}
	n, isInt := resolved.(ast.PdfInt)
	return int64(n), isInt
}

// samplePredictorLayout returns the layout a PNG predictor applies to the
// image's samples under, or nil when it cannot be known or the decoded data is
// not a whole number of its rows. ISO 32000-1, 8.9.5 lays the samples out row
// by row, each row padded to a whole byte, which is the row Table 8 defines
// for a predictor with /Colors components of /BitsPerComponent bits over
// /Columns samples. A partial final row is excluded because decoders disagree
// on it, and an exact layout makes the prediction undo to the same bytes in
// every one.
func samplePredictorLayout(pending map[int]ast.PdfObject, dict ast.PdfDict, width, height int64, decodedLen int) *predictorLayout {
	var colors, bpc int64
	if mask, _, _ := followPending(pending, dict["ImageMask"]); mask == ast.PdfBool(true) {
		// A stencil mask is one bit per sample with no colour space
		// (ISO 32000-1, 8.9.6.2).
		colors, bpc = 1, 1
	} else {
		var ok bool
		if bpc, ok = pendingInt(pending, dict["BitsPerComponent"]); !ok {
			return nil
		}
		if colors, ok = colorComponents(pending, dict["ColorSpace"]); !ok {
			return nil
		}
	}
	switch bpc {
	case 1, 2, 4, 8, 16:
	default:
		return nil
	}
	rowBytes := (width*colors*bpc + 7) / 8
	if rowBytes < 1 || int64(decodedLen)/rowBytes != height || int64(decodedLen)%rowBytes != 0 {
		return nil
	}
	return &predictorLayout{colors: intconv.Clamp(colors), bpc: intconv.Clamp(bpc), columns: intconv.Clamp(width)}
}

// colorComponents returns the number of components of an image's colour space
// (ISO 32000-1, 8.6), or false for one it cannot count without resolving a
// resource or that has no fixed count.
func colorComponents(pending map[int]ast.PdfObject, cs ast.PdfObject) (int64, bool) {
	resolved, _, ok := followPending(pending, cs)
	if !ok {
		return 0, false
	}
	switch v := resolved.(type) {
	case ast.PdfName:
		switch v {
		case "DeviceGray", "G":
			return 1, true
		case "DeviceRGB", "RGB":
			return 3, true
		case "DeviceCMYK", "CMYK":
			return 4, true
		}
	case ast.PdfArray:
		if len(v) == 0 {
			return 0, false
		}
		family, _ := v[0].(ast.PdfName)
		switch family {
		case "CalGray", "Indexed", "I", "Separation":
			return 1, true
		case "CalRGB", "Lab":
			return 3, true
		case "ICCBased":
			if len(v) < 2 {
				return 0, false
			}
			ref, isRef := v[1].(ast.PdfIndirectRef)
			if !isRef {
				return 0, false
			}
			profile, isStream := pending[ref.ObjectNum].(ast.PdfStream)
			if !isStream {
				return 0, false
			}
			return pendingInt(pending, profile.Dict["N"])
		case "DeviceN":
			if len(v) < 2 {
				return 0, false
			}
			names, _, ok := followPending(pending, v[1])
			if arr, isArr := names.(ast.PdfArray); ok && isArr && len(arr) > 0 {
				return int64(len(arr)), true
			}
		}
	}
	return 0, false
}

// encodeCandidate returns decoded compressed with FlateDecode at the zlib
// level given, after PNG prediction under layout when it is not nil, and the image
// dictionary that describes the result: dict with /Filter and /DecodeParms
// replaced and every other entry kept.
func encodeCandidate(decoded []byte, dict ast.PdfDict, layout *predictorLayout, level int) ([]byte, ast.PdfDict, bool) {
	data := decoded
	if layout != nil {
		predicted, err := filter.EncodePNGPredictor(decoded, layout.colors, layout.bpc, layout.columns)
		if err != nil {
			return nil, nil, false
		}
		data = predicted
	}
	var buf bytes.Buffer
	zw, err := zlib.NewWriterLevel(&buf, level)
	if err != nil {
		return nil, nil, false
	}
	if _, err := zw.Write(data); err != nil {
		return nil, nil, false
	}
	if err := zw.Close(); err != nil {
		return nil, nil, false
	}

	out := maps.Clone(dict)
	out["Filter"] = ast.PdfName("FlateDecode")
	delete(out, "DecodeParms")
	if layout != nil {
		// Predictor 15 lets every row name its own PNG filter (ISO 32000-1,
		// Table 8), which is what EncodePNGPredictor writes.
		out["DecodeParms"] = ast.PdfDict{
			"Predictor":        ast.PdfInt(15),
			"Colors":           ast.PdfInt(layout.colors),
			"BitsPerComponent": ast.PdfInt(layout.bpc),
			"Columns":          ast.PdfInt(layout.columns),
		}
	}
	return buf.Bytes(), out, true
}

// serializedSize returns how many bytes s takes as written, dictionary and
// data together, without holding the serialization.
func serializedSize(ctx context.Context, s ast.PdfStream) (int64, error) {
	var c byteCounter
	return Serialize(ctx, &c, s)
}

// byteCounter is an io.Writer that discards what it is given.
type byteCounter struct{}

// Write discards p.
func (byteCounter) Write(p []byte) (int, error) { return len(p), nil }
