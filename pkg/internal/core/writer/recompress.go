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
// result is smaller than the stream it replaces. DCTDecode, JPXDecode, JBIG2Decode and
// CCITTFaxDecode images are left alone: each is a format of its own, and a
// Flate re-encoding would replace it with the decoded samples, a different
// trade rather than a lossless improvement of the same one.
//
// Only /Filter, /DecodeParms and the data change. /Decode, /SMask, /Mask,
// /ColorSpace, /BitsPerComponent, /Width, /Height and /DL keep their values,
// and every re-encoding is decoded again and compared with the original
// samples before it replaces them, so a stream whose samples would differ is
// never written.
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
// asks. Images are visited in object-number order and zlib output depends on
// its input alone, so the result does too.
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
	source, ok := directFilterKeys(pending, s.Dict)
	if !ok || !chainRecompressible(source["Filter"], dec) {
		return recompressionPlan{}, false, nil
	}

	width, okW := pendingInt(pending, s.Dict["Width"])
	height, okH := pendingInt(pending, s.Dict["Height"])
	if !okW || !okH || width < 1 || height < 1 || width > lim.MaxImagePixels/height {
		return recompressionPlan{}, false, nil
	}

	decoded, ok, err := decodeWhole(ctx, dec, ast.NewLazyStream(source, s.Bytes))
	if err != nil || !ok {
		return recompressionPlan{}, false, err
	}

	before, err := serializedSize(ctx, s)
	if err != nil {
		return recompressionPlan{}, false, ctx.Err()
	}

	// The layout is chosen at zlib's default level, which ranks the two
	// candidates as the best level does at a fraction of its cost, and only
	// the winner is compressed at the best level.
	candidates := []*predictorLayout{nil}
	if l := samplePredictorLayout(pending, s.Dict, width, height, len(decoded)); l != nil {
		candidates = append(candidates, l)
	}
	var (
		layout   *predictorLayout
		screened = -1
	)
	for _, l := range candidates {
		if err := ctx.Err(); err != nil {
			return recompressionPlan{}, false, err
		}
		encoded, _, ok := encodeCandidate(decoded, s.Dict, l, zlib.DefaultCompression)
		if ok && (screened < 0 || len(encoded) < screened) {
			layout, screened = l, len(encoded)
		}
	}
	if screened < 0 || int64(screened) > before+before/screenSlack {
		return recompressionPlan{}, false, nil
	}
	if err := ctx.Err(); err != nil {
		return recompressionPlan{}, false, err
	}
	encoded, dict, ok := encodeCandidate(decoded, s.Dict, layout, zlib.BestCompression)
	if !ok {
		return recompressionPlan{}, false, nil
	}
	after, err := serializedSize(ctx, ast.NewStream(dict, encoded))
	if err != nil {
		// The source serialized, and the replacement differs from it only in
		// encoding entries litho wrote, so only the context can fail it.
		return recompressionPlan{}, false, ctx.Err()
	}
	if after >= before {
		return recompressionPlan{}, false, nil
	}
	best := recompressionPlan{source: source, dict: dict, encoded: encoded, saved: before - after, layout: layout}

	// The re-encoding replaces the samples only if it decodes to them.
	again, ok, err := decodeWhole(ctx, dec, ast.NewStream(best.dict, best.encoded))
	if err != nil || !ok || !bytes.Equal(again, decoded) {
		return recompressionPlan{}, false, err
	}
	return best, true, nil
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
	source, dict, layout, want := p.source, p.dict, p.layout, len(p.encoded)
	return func(ctx context.Context) ([]byte, error) {
		decoded, ok, err := decodeWhole(ctx, dec, ast.NewLazyStream(source, raw))
		if err != nil {
			return nil, fmt.Errorf("decoding a recompressed image's source: %w", err)
		}
		if !ok {
			return nil, fmt.Errorf("%w: its source no longer decodes whole", ErrRecompressionDiverged)
		}
		encoded, _, ok := encodeCandidate(decoded, dict, layout, zlib.BestCompression)
		if !ok || len(encoded) != want {
			return nil, fmt.Errorf("%w: %d bytes where %d were measured", ErrRecompressionDiverged, len(encoded), want)
		}
		return encoded, nil
	}
}

// decodeWhole decodes s through its filter chain, reporting false when a
// filter was skipped or met damaged data, or when decoding failed for any
// reason but the context's. The decoder refuses salvaged data
// ([filter.Decoder.RefuseSalvaged]) so a stream litho reads leniently is
// never rewritten as though that reading were the file's.
func decodeWhole(ctx context.Context, dec *filter.Decoder, s ast.PdfStream) ([]byte, bool, error) {
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

// directFilterKeys returns dict with /Filter and /DecodeParms, and the
// elements of either when it is an array, resolved to direct values through
// pending, or false when one of them is neither direct nor a reference to a
// direct value. The decoder reads only direct filter keys; the source's own
// resolution of indirect ones went through object numbers this rewrite
// renumbered, so the pending objects are what they now name.
func directFilterKeys(pending map[int]ast.PdfObject, dict ast.PdfDict) (ast.PdfDict, bool) {
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
	return &predictorLayout{colors: int(colors), bpc: int(bpc), columns: int(width)}
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
