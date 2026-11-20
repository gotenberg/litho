package writer

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/filter/dct"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/models"
)

// ImageReduction configures [ReduceImages]: what it trades and the decoder
// and budgets that bound it.
type ImageReduction struct {
	// Decoder decodes the images' current filter chains, and its plugins
	// mark the images left alone. A nil Decoder decodes with the built-in
	// filters under the default budgets.
	Decoder *filter.Decoder
	// Limits bounds the pass: an image past MaxImagePixels, or whose samples
	// pass MaxStreamBytes, is left as it is, and MaxStreamBytes bounds the
	// re-encoded bytes the pass keeps in memory.
	Limits models.Limits
	// Quality is the JPEG quality, 1 to 100, continuous-tone images are
	// re-encoded at, as [dct.EncodeOptions.Quality] reads it; 0 re-encodes
	// none that is not downsampled.
	Quality int
	// MaxDPI is the resolution, in pixels an inch, an image drawn finer is
	// downsampled to; 0 downsamples none.
	MaxDPI float64
	// Logger takes, at Debug, what the pass changed and why it left each
	// image it left; nil logs nothing.
	Logger *slog.Logger
}

// ReduceStats reports what [ReduceImages] changed and what it left.
type ReduceStats struct {
	// Downsampled counts the images written at fewer pixels, JPEG counts
	// the images written as JPEG, Lossless those written with FlateDecode.
	Downsampled, JPEG, Lossless int
	// SavedBytes is how many bytes the replaced images are smaller by, as
	// serialized.
	SavedBytes int64
	// Kept counts the images left as they were, by reason.
	Kept map[string]int
	// Encoded holds the object numbers of the images written as JPEG, whose
	// Huffman tables are already fitted to them, so that [RecompressImages]
	// leaves them.
	Encoded map[int]bool
}

// downsampleThreshold is how many times finer than the target an image must
// be drawn for [ReduceImages] to downsample it: Ghostscript's default
// ColorImageDownsampleThreshold, GrayImageDownsampleThreshold and
// MonoImageDownsampleThreshold, 1.5, which Acrobat Distiller's parameters
// define (Adobe, Acrobat Distiller Parameters). An image within it keeps its
// pixels: averaging it down by less than a third would blur every edge by a
// fraction of a pixel for a saving of less than half its samples.
const downsampleThreshold = 1.5

// subsampleChromaBelow is the JPEG quality below which a three-component
// image's chroma is written at half resolution (4:2:0) and from which it is
// written in full. It is the rule of the recolour pass, ImageMagick's (2x2
// unless the quality is 90 or more): a quality that high asks for the
// colour edges 4:2:0 softens.
const subsampleChromaBelow = 90

// jpegWorkPerSample is how many bytes the JPEG encoder holds for each sample
// it encodes, beside the samples: a byte of the sample's component plane and
// two of its quantized coefficient ([dct.Encode]). An image is encoded as
// JPEG only when that working set fits the stream budget, which bounds what
// one decode holds, so the encoder never holds more than the decode it
// follows. Memory guard, not spec-mandated: at the default budget it admits
// 89 million samples, a photograph of 29 million pixels in RGB.
const jpegWorkPerSample = 3

// The reasons [ReduceImages] leaves an image as it is.
const (
	keptExternal      = "external data"
	keptFilterKeys    = "filter keys not direct"
	keptStencil       = "stencil mask"
	keptDepth         = "not 8 bits a component"
	keptIndexed       = "Indexed colour space"
	keptColourKey     = "colour-key mask"
	keptColourSpace   = "colour space unknown"
	keptDecode        = "Decode array malformed"
	keptFilter        = "filter litho does not re-encode"
	keptPlugin        = "filter a plugin claims"
	keptBudget        = "past the image budget"
	keptDamaged       = "data that does not decode whole"
	keptResolution    = "within the resolution"
	keptMeasure       = "measured in pixels"
	keptMatte         = "soft mask with a matte"
	keptSoftMask      = "soft mask, kept lossless"
	keptNoJPEGSpace   = "colour space JPEG does not hold"
	keptJPEGQuality   = "JPEG at or under the quality"
	keptJPEGUnread    = "JPEG whose tables do not read"
	keptNotSmaller    = "re-encoding not smaller"
	keptNothingAsked  = "no re-encoding asked"
	keptTooWideForDCT = "wider or taller than a JPEG frame"
)

// ReduceImages downsamples the images of the allocator drawn finer than
// cfg.MaxDPI and re-encodes continuous-tone images as JPEG at cfg.Quality,
// keeping each result only when it serializes smaller than the stream it
// replaces. It runs before [RecompressImages], which leaves the JPEGs it
// wrote, and on the plaintext objects, as every pass of [Compact] does.
//
// An image is downsampled when it is drawn, at its largest placement (see
// [measurePlacements]), at more than [downsampleThreshold] times cfg.MaxDPI
// along its coarser side; both sides are then scaled by one factor, so that
// the coarser takes cfg.MaxDPI ([targetSize]). The samples are averaged by area in the image's
// own colour space ([resampleArea]). A soft mask is downsampled by the same
// rule over the placements of the images it masks, so a mask and an image of
// the same size stay the same size, and it stays lossless (ISO 32000-1,
// 11.6.5.3). An image that is drawn nowhere the walk can see, or whose size
// the walk cannot vouch for, keeps its pixels.
//
// An image qualifies for JPEG (ISO 32000-1, 7.4.8 and 8.9.5.1) when it has
// 8 bits a component in DeviceGray, DeviceRGB, DeviceCMYK, CalGray, CalRGB
// or ICCBased of one, three or four components. Three components are
// written as YCbCr and four as CMYK, each with the Adobe marker that says so,
// which ISO 32000-1, 7.4.8 makes override /ColorTransform; /Decode, /Intent,
// /Interpolate and every other entry but the filter keys, the size and /DL
// keep their values, and the samples a reader decodes keep their meaning
// ([dct.Encode]). A JPEG that is not downsampled is re-encoded only when its
// own quality, read from its luminance table ([dct.Quality]), is above
// cfg.Quality, since a second generation of quantization loses detail at
// any quality; a downsampled JPEG is encoded at the lower of the two. The
// lossless encoding of the same samples is kept instead when it is no larger.
//
// An image is left alone, counted by reason in [ReduceStats.Kept], when it
// has external data, a stencil mask or 1, 2, 4 or 16 bits a component, an
// Indexed colour space (an average of indices is no colour of the palette),
// a colour-key /Mask (a colour that moves misses its key), a /Decode that is
// not two numbers a component, a filter other than the lossless ones of
// [RecompressImages] and DCTDecode alone (JPEG 2000, JBIG2 and CCITT data
// are formats of their own), a filter a plugin claims, samples past the image
// budget, or data that does not decode whole. Lab, Separation and DeviceN
// images are downsampled but never written as JPEG. An image with /Measure or
// /PtData, whose coordinates may count its pixels (ISO 32000-1, Table 89;
// ISO 32000-2, 12.10), and an image whose soft mask has a /Matte, which
// needs the two the same size (11.6.5.3), keep their pixels.
//
// Images are visited in object-number order, and the resampling, the JPEG
// encoding and zlib depend on their input alone, so the result does too.
// Re-encoded bytes are kept in memory up to the stream budget in total; past
// it an image's re-encoding is redone from its source when the stream is
// serialized, so the pass then holds one image at a time.
func ReduceImages(ctx context.Context, alloc *ObjAllocator, trailer ast.PdfDict, cfg ImageReduction) (ReduceStats, error) {
	stats := ReduceStats{Kept: make(map[string]int), Encoded: make(map[int]bool)}
	if cfg.Quality <= 0 && !(cfg.MaxDPI > 0) {
		return stats, nil
	}
	lim := limits.WithDefaults(cfg.Limits)
	pending := alloc.Pending()

	var places *placements
	if cfg.MaxDPI > 0 && !math.IsInf(cfg.MaxDPI, 1) {
		var err error
		if places, err = measurePlacements(ctx, pending, trailer, cfg.Decoder, lim); err != nil {
			return stats, err
		}
	}

	nums := make([]int, 0, len(pending))
	for num, obj := range pending {
		if s, ok := obj.(ast.PdfStream); ok && isImageStream(s.Dict) {
			nums = append(nums, num)
		}
	}
	slices.Sort(nums)
	softMasks, matted := maskRoles(pending, nums)

	var retained int64
	for _, num := range nums {
		if err := ctx.Err(); err != nil {
			return stats, fmt.Errorf("reducing images: %w", err)
		}
		s, ok := pending[num].(ast.PdfStream)
		if !ok {
			continue
		}
		in := reduceInput{
			num: num, stream: s, softMask: softMasks[num], matted: matted[num],
			places: places, cfg: cfg, lim: lim, pending: pending,
		}
		plan, kept, err := planReduction(ctx, in)
		if err != nil {
			return stats, fmt.Errorf("reducing image %d: %w", num, err)
		}
		if kept != "" {
			stats.Kept[kept]++
			continue
		}
		var replacement ast.PdfStream
		if size := int64(len(plan.encoded)); retained+size <= lim.MaxStreamBytes {
			retained += size
			replacement = ast.NewStream(plan.dict, plan.encoded)
		} else {
			replacement = ast.NewLazyStream(plan.dict, plan.redo(s.Bytes, cfg.Decoder))
		}
		alloc.AllocAt(num, replacement)
		stats.SavedBytes += plan.saved
		if plan.downsampled() {
			stats.Downsampled++
		}
		if plan.jpeg {
			stats.JPEG++
			stats.Encoded[num] = true
		} else {
			stats.Lossless++
		}
	}
	stats.log(cfg.Logger)
	return stats, nil
}

// log writes stats to logger at Debug, the reasons in order.
func (stats ReduceStats) log(logger *slog.Logger) {
	if logger == nil {
		return
	}
	attrs := []any{
		slog.Int("downsampled", stats.Downsampled),
		slog.Int("jpeg", stats.JPEG),
		slog.Int("lossless", stats.Lossless),
		slog.Int64("saved_bytes", stats.SavedBytes),
	}
	for _, reason := range slices.Sorted(maps.Keys(stats.Kept)) {
		attrs = append(attrs, slog.Int("kept: "+reason, stats.Kept[reason]))
	}
	logger.Debug("image reduction", attrs...)
}

// maskRoles returns the images that are the soft mask of another image, and
// those that are a soft mask with /Matte or the image such a mask is for:
// ISO 32000-1, 11.6.5.3 needs the two the same size.
func maskRoles(pending map[int]ast.PdfObject, nums []int) (soft, matted map[int]bool) {
	soft, matted = make(map[int]bool), make(map[int]bool)
	for _, num := range nums {
		s := pending[num].(ast.PdfStream)
		ref, ok := s.Dict["SMask"].(ast.PdfIndirectRef)
		if !ok {
			continue
		}
		mask, ok := pending[ref.ObjectNum].(ast.PdfStream)
		if !ok || !isImageStream(mask.Dict) {
			continue
		}
		soft[ref.ObjectNum] = true
		if _, ok := mask.Dict["Matte"]; ok {
			matted[ref.ObjectNum], matted[num] = true, true
		}
	}
	return soft, matted
}

// reduceInput is one image [planReduction] decides on.
type reduceInput struct {
	num      int
	stream   ast.PdfStream
	softMask bool
	matted   bool
	places   *placements
	cfg      ImageReduction
	lim      models.Limits
	pending  map[int]ast.PdfObject
}

// reducePlan is the re-encoding [planReduction] chose for one image.
type reducePlan struct {
	// source is the image's dictionary with its filter keys made direct,
	// the view the decoder reads its current encoding through.
	source                    ast.PdfDict
	dict                      ast.PdfDict
	encoded                   []byte
	saved                     int64
	width, height, comps      int
	targetWidth, targetHeight int
	jpeg                      bool
	quality                   int
	layout                    *predictorLayout
}

// downsampled reports whether the plan writes fewer pixels than the source.
func (p reducePlan) downsampled() bool {
	return p.targetWidth < p.width || p.targetHeight < p.height
}

// sampleSpace is what [planReduction] needs of an image's colour space: its
// components, whether a JPEG holds it, and the reason it is left alone, ""
// when it is not.
type sampleSpace struct {
	comps  int
	jpeg   bool
	reason string
}

// imageSpace reads an image's colour space (ISO 32000-1, 8.6), through one
// reference into pending.
func imageSpace(pending map[int]ast.PdfObject, cs ast.PdfObject) sampleSpace {
	resolved, _, ok := followPending(pending, cs)
	if !ok {
		return sampleSpace{reason: keptColourSpace}
	}
	switch v := resolved.(type) {
	case ast.PdfName:
		switch v {
		case "DeviceGray", "G":
			return sampleSpace{comps: 1, jpeg: true}
		case "DeviceRGB", "RGB":
			return sampleSpace{comps: 3, jpeg: true}
		case "DeviceCMYK", "CMYK":
			return sampleSpace{comps: 4, jpeg: true}
		}
	case ast.PdfArray:
		if len(v) == 0 {
			break
		}
		family, _ := v[0].(ast.PdfName)
		switch family {
		case "Indexed", "I":
			return sampleSpace{reason: keptIndexed}
		case "CalGray":
			return sampleSpace{comps: 1, jpeg: true}
		case "CalRGB":
			return sampleSpace{comps: 3, jpeg: true}
		case "Lab":
			return sampleSpace{comps: 3}
		case "Separation":
			return sampleSpace{comps: 1}
		case "DeviceN":
			if n, ok := colorComponents(pending, cs); ok && n >= 1 && n <= 32 {
				return sampleSpace{comps: int(n)} // fits int: at most 32, checked
			}
		case "ICCBased":
			if n, ok := colorComponents(pending, cs); ok && (n == 1 || n == 3 || n == 4) {
				return sampleSpace{comps: int(n), jpeg: true} // fits int: 1, 3 or 4
			}
		}
	}
	return sampleSpace{reason: keptColourSpace}
}

// planReduction decides what becomes of one image and does it: the plan
// when the image is replaced, else the reason it is left alone. The error is
// only ever the context's.
func planReduction(ctx context.Context, in reduceInput) (reducePlan, string, error) {
	s, pending := in.stream, in.pending
	if _, external := s.Dict["F"]; external {
		return reducePlan{}, keptExternal, nil
	}
	source, ok := DirectFilterKeys(pending, s.Dict)
	if !ok {
		return reducePlan{}, keptFilterKeys, nil
	}
	if mask, _, _ := followPending(pending, s.Dict["ImageMask"]); mask == ast.PdfBool(true) {
		return reducePlan{}, keptStencil, nil
	}
	if bpc, ok := pendingInt(pending, s.Dict["BitsPerComponent"]); !ok || bpc != 8 {
		return reducePlan{}, keptDepth, nil
	}
	space := imageSpace(pending, s.Dict["ColorSpace"])
	if space.reason != "" {
		return reducePlan{}, space.reason, nil
	}
	if key, _, _ := followPending(pending, s.Dict["Mask"]); key != nil {
		if _, isArray := key.(ast.PdfArray); isArray {
			return reducePlan{}, keptColourKey, nil
		}
	}
	if !decodeArrayFits(pending, s.Dict["Decode"], space.comps) {
		return reducePlan{}, keptDecode, nil
	}
	dctSource := chainIsDCT(source["Filter"], in.cfg.Decoder)
	if !dctSource && !chainRecompressible(source["Filter"], in.cfg.Decoder) {
		if pluginClaims(source["Filter"], in.cfg.Decoder) {
			return reducePlan{}, keptPlugin, nil
		}
		return reducePlan{}, keptFilter, nil
	}
	width, okW := pendingInt(pending, s.Dict["Width"])
	height, okH := pendingInt(pending, s.Dict["Height"])
	if !okW || !okH || width < 1 || height < 1 || width > in.lim.MaxImagePixels/height ||
		width*height > in.lim.MaxStreamBytes/int64(space.comps) {
		return reducePlan{}, keptBudget, nil
	}
	// width x height x comps is at most MaxStreamBytes, checked above, which
	// models.Limits bounds by the int range, so neither side saturates.
	w, h := intconv.Clamp(width), intconv.Clamp(height)

	tw, th, reason := in.target(w, h)
	jpegOK := space.jpeg && !in.softMask
	downsample := tw < w || th < h
	if !downsample {
		switch {
		case in.cfg.Quality <= 0:
			return reducePlan{}, reason, nil
		case in.softMask:
			return reducePlan{}, keptSoftMask, nil
		case !jpegOK:
			return reducePlan{}, keptNoJPEGSpace, nil
		}
	}

	quality := in.cfg.Quality
	if dctSource {
		raw, err := s.Bytes(ctx)
		if err != nil {
			return reducePlan{}, keptDamaged, ctx.Err()
		}
		own, err := dct.Quality(ctx, raw)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return reducePlan{}, "", ctxErr
			}
			return reducePlan{}, keptJPEGUnread, nil
		}
		if !downsample && own <= quality {
			return reducePlan{}, keptJPEGQuality, nil
		}
		if quality <= 0 || own < quality {
			quality = own
		}
	}
	if jpegOK && quality > 0 && (tw > dct.MaxDimension || th > dct.MaxDimension) {
		if !downsample {
			return reducePlan{}, keptTooWideForDCT, nil
		}
		quality = 0
	}
	if jpegOK && quality > 0 && int64(tw)*int64(th)*int64(space.comps) > in.lim.MaxStreamBytes/jpegWorkPerSample {
		if !downsample {
			return reducePlan{}, keptBudget, nil
		}
		quality = 0
	}
	if !jpegOK {
		quality = 0
	}

	plan := reducePlan{
		source: source, width: w, height: h, comps: space.comps,
		targetWidth: tw, targetHeight: th,
	}
	samples, ok, err := plan.samples(ctx, s.Bytes, in.cfg.Decoder)
	if err != nil || !ok {
		return reducePlan{}, keptDamaged, err
	}
	before, err := serializedSize(ctx, s)
	if err != nil {
		return reducePlan{}, "", ctx.Err()
	}
	if err := plan.choose(ctx, samples, s.Dict, quality, dctSource && !downsample); err != nil {
		return reducePlan{}, "", err
	}
	if plan.encoded == nil {
		return reducePlan{}, keptNotSmaller, nil
	}
	after, err := serializedSize(ctx, ast.NewStream(plan.dict, plan.encoded))
	if err != nil {
		return reducePlan{}, "", ctx.Err()
	}
	if after >= before {
		return reducePlan{}, keptNotSmaller, nil
	}
	// The re-encoding replaces the image only if it decodes whole, to as
	// many samples as the dictionary now says it holds.
	again, ok, err := DecodeWhole(ctx, in.cfg.Decoder, ast.NewStream(plan.dict, plan.encoded))
	if err != nil {
		return reducePlan{}, "", err
	}
	if !ok || len(again) != tw*th*space.comps {
		return reducePlan{}, keptNotSmaller, nil
	}
	plan.saved = before - after
	return plan, "", nil
}

// target returns the size an image of w x h pixels is written at, and the
// reason it keeps its size when it does.
func (in reduceInput) target(w, h int) (int, int, string) {
	if in.places == nil {
		return w, h, keptNothingAsked
	}
	if _, measured := in.stream.Dict["Measure"]; measured {
		return w, h, keptMeasure
	}
	if _, measured := in.stream.Dict["PtData"]; measured {
		return w, h, keptMeasure
	}
	if in.matted {
		return w, h, keptMatte
	}
	size, reason, ok := in.places.size(in.num)
	if !ok {
		return w, h, string(reason)
	}
	tw, th := targetSize(w, h, size.width, size.height, in.cfg.MaxDPI)
	if tw == w && th == h {
		return w, h, keptResolution
	}
	return tw, th, ""
}

// targetSize returns the size an image of w x h pixels takes at dpi pixels
// an inch when its sides are drawn at most width and height inches long. Its
// resolution is the coarser of the two sides': pixels over inches. At no
// more than [downsampleThreshold] times dpi the image keeps its size; past
// it both sides are scaled by dpi over that resolution, rounded up, at
// least 1 pixel. One factor keeps the image's shape, as a caller reading
// its pixels expects, and leaves the finer side above dpi rather than any
// side under it.
func targetSize(w, h int, width, height, dpi float64) (int, int) {
	if !(width > 0) || !(height > 0) {
		return w, h
	}
	res := math.Min(float64(w)/width, float64(h)/height)
	if res <= float64(downsampleThreshold*dpi) {
		return w, h
	}
	scale := dpi / res
	return scaledSide(w, scale), scaledSide(h, scale)
}

// pixelSlack is how far past a whole number of pixels a scaled side may fall
// and still round to it: the products that size a placement carry rounding
// errors of their own, and 150.00000000000003 pixels are 150. Not a
// resource cap: it moves a side by at most one pixel.
const pixelSlack = 1e-6

// scaledSide returns n pixels scaled by scale, below 1, rounded up past
// [pixelSlack] and at least 1.
func scaledSide(n int, scale float64) int {
	t := math.Ceil(float64(float64(n)*scale) - pixelSlack)
	if t < 1 {
		return 1
	}
	if t >= float64(n) {
		return n
	}
	return int(t) // fits int: below n, checked above
}

// decodeArrayFits reports whether a /Decode entry is absent or an array of
// two numbers a component (ISO 32000-1, 8.9.5.2, Table 90), which a JPEG and
// a resampling keep: both leave each sample a value of its component, and a
// decode array maps each value on its own.
func decodeArrayFits(pending map[int]ast.PdfObject, decode ast.PdfObject, comps int) bool {
	if decode == nil {
		return true
	}
	resolved, _, ok := followPending(pending, decode)
	if !ok {
		return false
	}
	if _, null := resolved.(ast.PdfNull); null {
		return true
	}
	arr, ok := resolved.(ast.PdfArray)
	if !ok || len(arr) != 2*comps {
		return false
	}
	for _, v := range arr {
		if _, ok := document.Number(v); !ok {
			return false
		}
	}
	return true
}

// pluginClaims reports whether a plugin of dec claims a filter of a /Filter
// value, already direct.
func pluginClaims(f ast.PdfObject, dec *filter.Decoder) bool {
	switch v := f.(type) {
	case ast.PdfName:
		return dec.Handles(v)
	case ast.PdfArray:
		for _, elem := range v {
			if name, ok := elem.(ast.PdfName); ok && dec.Handles(name) {
				return true
			}
		}
	}
	return false
}

// samples decodes the image's source and returns its samples resampled to
// the plan's size, false when the source does not decode whole to the
// samples its dictionary declares. Data past the declared samples is
// dropped, as readers ignore it (ISO 32000-1, 8.9.5.1).
func (p reducePlan) samples(ctx context.Context, raw func(context.Context) ([]byte, error), dec *filter.Decoder) ([]byte, bool, error) {
	decoded, ok, err := DecodeWhole(ctx, dec, ast.NewLazyStream(p.source, raw))
	if err != nil || !ok {
		return nil, false, err
	}
	want := int64(p.width) * int64(p.height) * int64(p.comps)
	if int64(len(decoded)) < want {
		return nil, false, nil
	}
	decoded = decoded[:want]
	if !p.downsampled() {
		return decoded, true, nil
	}
	out, err := resampleArea(ctx, decoded, p.width, p.height, p.comps, p.targetWidth, p.targetHeight)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, false, ctxErr
		}
		return nil, false, nil //nolint:nilerr // Dimensions the pass checked never fail; an image that does is left as it is.
	}
	return out, true, nil
}

// choose encodes samples, the plan's target size, as JPEG at quality when
// quality is above 0, and losslessly unless onlyJPEG; it keeps the smaller,
// the lossless one when they tie, and leaves the plan's encoding nil when
// neither encodes. dict is the image's dictionary.
func (p *reducePlan) choose(ctx context.Context, samples []byte, dict ast.PdfDict, quality int, onlyJPEG bool) error {
	var jpeg []byte
	if quality > 0 {
		data, err := encodeJPEG(ctx, samples, p.targetWidth, p.targetHeight, p.comps, quality)
		if err != nil {
			return err
		}
		jpeg = data
	}
	if !onlyJPEG {
		limit := -1
		if jpeg != nil {
			limit = len(jpeg)
		}
		encoded, layout, ok, err := encodeLossless(samples, &predictorLayout{colors: p.comps, bpc: 8, columns: p.targetWidth}, limit)
		if err != nil {
			return err
		}
		if ok {
			p.encoded, p.layout = encoded, layout
			p.dict = p.sizedDict(dict, "FlateDecode", layout)
			return nil
		}
	}
	if jpeg != nil {
		p.encoded, p.jpeg, p.quality = jpeg, true, quality
		p.dict = p.sizedDict(dict, "DCTDecode", nil)
	}
	return nil
}

// sizedDict returns dict with the filter keys of the plan's encoding, its
// size, and /DL counting its decoded bytes when dict has one (ISO 32000-1,
// 7.3.8.2, Table 5).
func (p reducePlan) sizedDict(dict ast.PdfDict, filterName ast.PdfName, layout *predictorLayout) ast.PdfDict {
	out := maps.Clone(dict)
	out["Filter"] = filterName
	delete(out, "DecodeParms")
	if layout != nil {
		out["DecodeParms"] = ast.PdfDict{
			"Predictor":        ast.PdfInt(15),
			"Colors":           ast.PdfInt(layout.colors),
			"BitsPerComponent": ast.PdfInt(layout.bpc),
			"Columns":          ast.PdfInt(layout.columns),
		}
	}
	out["Width"] = ast.PdfInt(p.targetWidth)
	out["Height"] = ast.PdfInt(p.targetHeight)
	if _, ok := out["DL"]; ok {
		out["DL"] = ast.PdfInt(int64(p.targetWidth) * int64(p.targetHeight) * int64(p.comps))
	}
	return out
}

// encodeJPEG writes samples as a baseline JPEG at quality, with Huffman
// tables fitted to the image, and chroma at half resolution below
// [subsampleChromaBelow]. The error is only ever the context's: dimensions
// the pass checked encode.
func encodeJPEG(ctx context.Context, samples []byte, w, h, comps, quality int) ([]byte, error) {
	opts := dct.EncodeOptions{Quality: quality, OptimizeHuffman: true}
	if quality < subsampleChromaBelow {
		opts.Subsampling = dct.Subsample420
	}
	data, err := dct.Encode(ctx, samples, w, h, comps, opts)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("encoding a reduced image: %w", err)
	}
	return data, nil
}

// encodeLossless compresses samples with FlateDecode, as they are or after
// the PNG predictor of layout, whichever is smaller (ISO 32000-1, 7.4.4.4),
// and reports false when both pass limit, a negative limit for none. It
// returns the layout the encoding used, nil for none.
func encodeLossless(samples []byte, layout *predictorLayout, limit int) ([]byte, *predictorLayout, bool, error) {
	predicted, err := filter.EncodePNGPredictor(samples, layout.colors, layout.bpc, layout.columns)
	if err != nil {
		return nil, nil, false, fmt.Errorf("predicting a reduced image: %w", err)
	}
	pred, okPred, err := filter.CompressFlate(predicted, limit)
	if err != nil {
		return nil, nil, false, fmt.Errorf("compressing a reduced image: %w", err)
	}
	if okPred {
		limit = len(pred) - 1
	}
	plain, okPlain, err := filter.CompressFlate(samples, limit)
	if err != nil {
		return nil, nil, false, fmt.Errorf("compressing a reduced image: %w", err)
	}
	switch {
	case okPlain:
		return plain, nil, true, nil
	case okPred:
		return pred, layout, true, nil
	}
	return nil, nil, false, nil
}

// redo returns a loader that redoes the plan's re-encoding from the image's
// source bytes, for an image whose re-encoding [ReduceImages] did not keep in
// memory. The result must have the length the plan measured, since the pass
// chose it on that size: any other length means the source changed under it.
func (p reducePlan) redo(raw func(context.Context) ([]byte, error), dec *filter.Decoder) func(context.Context) ([]byte, error) {
	want := len(p.encoded)
	return func(ctx context.Context) ([]byte, error) {
		samples, ok, err := p.samples(ctx, raw, dec)
		if err != nil {
			return nil, fmt.Errorf("decoding a reduced image's source: %w", err)
		}
		if !ok {
			return nil, fmt.Errorf("%w: its source no longer decodes whole", ErrRecompressionDiverged)
		}
		var encoded []byte
		if p.jpeg {
			encoded, err = encodeJPEG(ctx, samples, p.targetWidth, p.targetHeight, p.comps, p.quality)
		} else {
			var layout *predictorLayout
			encoded, layout, ok, err = encodeLossless(samples, &predictorLayout{colors: p.comps, bpc: 8, columns: p.targetWidth}, -1)
			if err == nil && (!ok || (layout == nil) != (p.layout == nil)) {
				err = fmt.Errorf("%w: its lossless encoding failed", ErrRecompressionDiverged)
			}
		}
		if err != nil {
			return nil, err
		}
		if len(encoded) != want {
			return nil, fmt.Errorf("%w: %d bytes where %d were measured", ErrRecompressionDiverged, len(encoded), want)
		}
		return encoded, nil
	}
}
