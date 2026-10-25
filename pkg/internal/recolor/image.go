package recolor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
)

// Colors supplies the colour conversion images are converted through, to the
// one target space of an [ImageConfig].
type Colors interface {
	// Converter returns the converter from the colour space space, a
	// /ColorSpace value as an image dictionary holds it, to the target. The
	// space is read against resources, whose /DefaultGray, /DefaultRGB and
	// /DefaultCMYK stand in for the device spaces (ISO 32000-1, 8.6.5.6),
	// on the page dictionary page, whose /OutputIntents replace the
	// document's (ISO 32000-2, 14.11.5), under the rendering intent intent,
	// "" for the graphics state's default (8.6.5.8). Either dictionary may
	// be nil.
	Converter(ctx context.Context, space ast.PdfObject, resources, page ast.PdfDict, intent ast.PdfName) (Converter, error)
	// SpotSpace returns the Separation or DeviceN space it is given with its
	// alternate space replaced by the target and its tint transform
	// rewritten to match: the space a kept spot colour is written in
	// (8.6.6.4, 8.6.6.5). Asked twice for one space, it returns the same
	// object, so every image and operator painting the spot keeps naming one
	// space.
	SpotSpace(ctx context.Context, space ast.PdfObject, resources, page ast.PdfDict) (ast.PdfObject, error)
}

// ImageConfig says what an [Images] converts to.
type ImageConfig struct {
	// Colors converts colours to the target.
	Colors Colors
	// Space is the /ColorSpace a converted image XObject is written in: a
	// device family name, or an array naming the target's ICC profile
	// stream, [/ICCBased ref]. A device family name denotes the device space
	// only where the resources drawing the image hold no default colour
	// space of that family (ISO 32000-1, 8.6.5.6): the content conversion
	// removes those, or a converted image reads through them.
	Space ast.PdfObject
	// InlineSpace is the /CS a converted inline image is written with: a
	// device family name, or the name of a /ColorSpace resource holding
	// Space in the resources of the content stream that draws the image
	// (ISO 32000-1, 8.9.7). Nil takes Space when it is a name, and refuses
	// to convert an inline image's samples otherwise.
	InlineSpace ast.PdfObject
	// Components is the number of components of Space.
	Components int
	// ConvertSpots converts a Separation or DeviceN image to Space through
	// its tint transform, where by default it keeps its tints and only its
	// alternate space is converted.
	ConvertSpots bool
	// Intent is the rendering intent of an image whose dictionary states
	// none (ISO 32000-1, 8.6.5.8), "" for relative colorimetric.
	Intent ast.PdfName
	// JPEGQuality, 1 to 100, re-encodes an image whose source filter is
	// DCTDecode or JPXDecode, once converted at 8 bits a component, as a
	// baseline JPEG at that quality (see [dct.Encode]) instead of
	// losslessly: a codec's samples, kept exactly, compress several times
	// worse under FlateDecode. 0 keeps every image lossless. A JPXDecode
	// source with a component deeper than 8 bits stays lossless (see
	// [Images.jpegSource]), and an image's /SMask and /Mask are kept as the
	// lossless re-encoding keeps them.
	JPEGQuality int
}

// ImageOutcome says what converting an image did.
type ImageOutcome uint8

const (
	// ImageUnchanged is an image left as it was: one already in the target
	// space, a stencil mask, which has no colour of its own (ISO 32000-1,
	// 8.9.6.2), or an image whose colorants are all None, which paints
	// nothing (8.6.6.4).
	ImageUnchanged ImageOutcome = iota
	// ImageConverted is an image whose samples were converted and
	// re-encoded.
	ImageConverted
	// ImagePalette is an Indexed image whose colour table was converted,
	// its samples, the indices, kept as they were (8.6.6.3).
	ImagePalette
	// ImageSpotKept is a Separation or DeviceN image whose tints were kept
	// and whose alternate space was converted.
	ImageSpotKept
)

// EncodedSize sums the encoded bytes of the images converted from one
// source filter, before and after.
type EncodedSize struct {
	Images        int
	Before, After int64
}

// ImageReport counts what an [Images] did.
type ImageReport struct {
	Converted, Palettes, SpotsKept, Unchanged, Refused int
	// Encoded sums the encoded sizes of the converted images by the last
	// filter of the source, "" for none. A JPXDecode or DCTDecode image
	// comes out under FlateDecode, which litho can encode losslessly, and
	// grows: a lossy codec's samples are what a lossless one keeps, unless
	// [ImageConfig.JPEGQuality] re-encodes it as JPEG.
	Encoded map[string]EncodedSize
}

// Images converts the images of one document to the target of an
// [ImageConfig], writing the converted image XObjects into an allocator.
//
// An image XObject is converted once, by object number, however many pages,
// forms or patterns draw it: a second request returns the first outcome. An
// image drawn under two sets of resources whose default colour spaces differ
// is converted under the first.
//
// Images is safe for concurrent use; conversions run one at a time, since
// they share the allocator.
type Images struct {
	doc   *document.Document
	alloc *writer.ObjAllocator
	cfg   ImageConfig

	mu      sync.Mutex
	done    map[int]imageResult
	indexed map[string]ast.PdfObject
	report  ImageReport
	// sources holds the stream each converted image XObject was read from,
	// which [Images.convertCopy] converts again.
	sources map[int]ast.PdfStream
}

// variantKeyer is a [Colors] whose conversions vary within one recolouring,
// as a session's do with a graphics state's black point compensation: its
// key spells the current variant, which caches of converted colour carry.
type variantKeyer interface {
	variantKey() string
}

// variantKey returns the current variant of the configured colours, "" when
// they have none.
func (im *Images) variantKey() string {
	if v, ok := im.cfg.Colors.(variantKeyer); ok {
		return v.variantKey()
	}
	return ""
}

// convertCopy converts the image XObject ref names, drawn with resources,
// as it was before the conversion, into a new object, and returns its
// reference, or false when the conversion keeps the image as it is (a
// stencil mask, an image in the target, a kept spot colour). The image's
// own conversion is untouched; its /Alternates are those of the image's own
// conversion. A copy counts in no [ImageReport] count but its encoded sizes.
func (im *Images) convertCopy(ctx context.Context, ref ast.PdfIndirectRef, resources ast.PdfDict) (ast.PdfIndirectRef, bool, error) {
	im.mu.Lock()
	defer im.mu.Unlock()
	stream, ok := im.sources[ref.ObjectNum]
	if !ok {
		var err error
		if stream, err = im.latestStream(ctx, ref); err != nil {
			return ast.PdfIndirectRef{}, false, fmt.Errorf("%w: image %v: %w", ErrImageNotConverted, ref, err)
		}
	}
	label := ast.PdfName(fmt.Sprintf("%d %d R", ref.ObjectNum, ref.GenNum))
	out, outcome, err := im.convertStream(ctx, label, stream, resources, nil, false)
	if err != nil {
		return ast.PdfIndirectRef{}, false, fmt.Errorf("image %v: %w", ref, err)
	}
	switch outcome {
	case ImageConverted, ImagePalette:
		return im.alloc.Alloc(out), true, nil
	case ImageUnchanged, ImageSpotKept:
	}
	return ast.PdfIndirectRef{}, false, nil
}

type imageResult struct {
	outcome ImageOutcome
	err     error
}

// NewImages returns an [Images] converting the images of doc, writing into
// alloc.
func NewImages(doc *document.Document, alloc *writer.ObjAllocator, cfg ImageConfig) (*Images, error) {
	if cfg.Colors == nil || cfg.Space == nil {
		return nil, errors.New("recolor: an image conversion needs a target space and its colours")
	}
	if cfg.Components < 1 || cfg.Components > maxImageComponents {
		return nil, fmt.Errorf("recolor: a target of %d components", cfg.Components)
	}
	if cfg.JPEGQuality < 0 || cfg.JPEGQuality > 100 {
		return nil, fmt.Errorf("recolor: a JPEG quality of %d, outside 1 to 100", cfg.JPEGQuality)
	}
	if cfg.InlineSpace == nil {
		if name, ok := cfg.Space.(ast.PdfName); ok {
			cfg.InlineSpace = name
		}
	}
	return &Images{
		doc:     doc,
		alloc:   alloc,
		cfg:     cfg,
		done:    make(map[int]imageResult),
		indexed: make(map[string]ast.PdfObject),
		sources: make(map[int]ast.PdfStream),
		report:  ImageReport{Encoded: make(map[string]EncodedSize)},
	}, nil
}

// Report returns what the conversions so far did.
func (im *Images) Report() ImageReport {
	im.mu.Lock()
	defer im.mu.Unlock()
	r := im.report
	r.Encoded = maps.Clone(im.report.Encoded)
	return r
}

// ConvertXObject converts the image XObject ref names, drawn with the
// resource dictionary resources on the page dictionary page, and queues the
// converted stream at the image's own object number, so every reference to
// it sees the conversion. An image the allocator already holds a newer
// version of is read from there. The images of its /Alternates (ISO 32000-1,
// 8.9.5.4) are converted with it.
//
// An image it cannot convert exactly is left as it was, with an error
// wrapping [ErrImageNotConverted].
func (im *Images) ConvertXObject(ctx context.Context, ref ast.PdfIndirectRef, resources, page ast.PdfDict) (ImageOutcome, error) {
	im.mu.Lock()
	defer im.mu.Unlock()
	return im.convertRef(ctx, ref, resources, page)
}

func (im *Images) convertRef(ctx context.Context, ref ast.PdfIndirectRef, resources, page ast.PdfDict) (ImageOutcome, error) {
	if r, ok := im.done[ref.ObjectNum]; ok {
		return r.outcome, r.err
	}
	// Marked before the work, so an /Alternates entry naming the image
	// itself ends here.
	im.done[ref.ObjectNum] = imageResult{outcome: ImageUnchanged}
	outcome, err := im.convertRefOnce(ctx, ref, resources, page)
	if ctxErr := ctx.Err(); ctxErr != nil {
		// A cancelled conversion is not an outcome of the image.
		delete(im.done, ref.ObjectNum)
		return ImageUnchanged, ctxErr
	}
	im.done[ref.ObjectNum] = imageResult{outcome: outcome, err: err}
	im.count(outcome, err)
	return outcome, err
}

func (im *Images) convertRefOnce(ctx context.Context, ref ast.PdfIndirectRef, resources, page ast.PdfDict) (ImageOutcome, error) {
	stream, err := im.latestStream(ctx, ref)
	if err != nil {
		return ImageUnchanged, fmt.Errorf("%w: image %v: %w", ErrImageNotConverted, ref, err)
	}
	im.sources[ref.ObjectNum] = stream
	label := ast.PdfName(fmt.Sprintf("%d %d R", ref.ObjectNum, ref.GenNum))
	out, outcome, err := im.convertStream(ctx, label, stream, resources, page, false)
	if err != nil {
		return ImageUnchanged, fmt.Errorf("image %v: %w", ref, err)
	}
	if outcome != ImageUnchanged {
		if !im.alloc.AllocAt(ref.ObjectNum, out) {
			return ImageUnchanged, fmt.Errorf("%w: image %v has no object number to write it at", ErrImageNotConverted, ref)
		}
	}
	im.convertAlternates(ctx, out.Dict, resources, page)
	return outcome, nil
}

// latestStream returns the image stream ref names, as the allocator holds it
// when an earlier edit queued it, else as the document does.
func (im *Images) latestStream(ctx context.Context, ref ast.PdfIndirectRef) (ast.PdfStream, error) {
	if obj, ok := im.alloc.Pending()[ref.ObjectNum]; ok {
		if s, ok := obj.(ast.PdfStream); ok && s.Dict != nil {
			return s, nil
		}
		return ast.PdfStream{}, fmt.Errorf("object %d is not a stream", ref.ObjectNum)
	}
	s, err := document.ResolveStream(ctx, im.doc, ref)
	if err != nil {
		return ast.PdfStream{}, err
	}
	if s.Dict == nil {
		return ast.PdfStream{}, fmt.Errorf("object %d is not a stream", ref.ObjectNum)
	}
	return s, nil
}

// convertAlternates converts the images an image's /Alternates names (ISO
// 32000-1, 8.9.5.4, Table 91). An alternate that fails is left as it was and
// logged: it fails as its own image and does not undo the image's
// conversion.
func (im *Images) convertAlternates(ctx context.Context, dict ast.PdfDict, resources, page ast.PdfDict) {
	arr, err := document.ResolveArray(ctx, im.doc, dict["Alternates"])
	if err != nil {
		return
	}
	for _, entry := range arr {
		alt, err := document.ResolveDict(ctx, im.doc, entry)
		if err != nil || alt == nil {
			continue
		}
		ref, ok := alt["Image"].(ast.PdfIndirectRef)
		if !ok {
			continue
		}
		if _, err := im.convertRef(ctx, ref, resources, page); err != nil {
			im.doc.Logger.Debug("recolor: leaving an alternate image as it was", slog.Any("err", err))
		}
	}
}

func (im *Images) count(outcome ImageOutcome, err error) {
	if err != nil {
		im.report.Refused++
		return
	}
	switch outcome {
	case ImageConverted:
		im.report.Converted++
	case ImagePalette:
		im.report.Palettes++
	case ImageSpotKept:
		im.report.SpotsKept++
	case ImageUnchanged:
		im.report.Unchanged++
	}
}

// convertStream converts one image stream, an XObject's or an inline
// image's written out in full. The result is a new stream; stream is not
// edited.
func (im *Images) convertStream(ctx context.Context, label ast.PdfName, stream ast.PdfStream, resources, page ast.PdfDict, inline bool) (ast.PdfStream, ImageOutcome, error) {
	dict := stream.Dict
	// ISO 32000-1, 8.9.6.2: a stencil mask paints the current colour
	// through its shape and has no colour of its own. An /ImageMask that
	// does not resolve counts as absent, as the image decoder reads it.
	if isMask, err := document.ResolveBool(ctx, im.doc, dict["ImageMask"]); err == nil && bool(isMask) {
		return stream, ImageUnchanged, nil
	}
	csObj, stated := dict["ColorSpace"]
	if !stated {
		// A JPXDecode image may leave /ColorSpace out and take its colour
		// from the JPEG 2000 data (ISO 32000-1, 7.4.9); any other image
		// must state one (8.9.5, Table 89).
		if lastFilter(ctx, im.doc, dict) == "JPXDecode" {
			return im.convertJPXColour(ctx, label, stream, resources, page, inline)
		}
		return stream, ImageUnchanged, fmt.Errorf("%w: it states no /ColorSpace", ErrImageNotConverted)
	}
	sp, err := describeImageSpace(ctx, im.doc, csObj)
	if err != nil {
		return stream, ImageUnchanged, fmt.Errorf("%w: %w", ErrImageNotConverted, err)
	}
	if paintsNothing(ctx, im.doc, sp) {
		return stream, ImageUnchanged, nil
	}
	if im.inTarget(ctx, sp, resources) {
		return stream, ImageUnchanged, nil
	}
	switch sp.family {
	case familyIndexed:
		return im.convertIndexed(ctx, stream, sp, resources, page, inline)
	case familySeparation, familyDeviceN:
		if !im.cfg.ConvertSpots {
			space, err := im.cfg.Colors.SpotSpace(ctx, sp.obj, resources, page)
			if err != nil {
				return stream, ImageUnchanged, fmt.Errorf("%w: keeping its spot colour: %w", ErrImageNotConverted, err)
			}
			return withColorSpace(stream, space), ImageSpotKept, nil
		}
	}
	return im.convertPixels(ctx, label, stream, sp, resources, page, inline)
}

// withColorSpace returns stream with its /ColorSpace replaced and its data,
// as encoded, kept.
func withColorSpace(stream ast.PdfStream, space ast.PdfObject) ast.PdfStream {
	out := stream
	out.Dict = maps.Clone(stream.Dict)
	out.Dict["ColorSpace"] = space
	return out
}

// paintsNothing reports a Separation space over the colorant None, or a
// DeviceN space naming only None, which never marks the page (ISO 32000-1,
// 8.6.6.4 and 8.6.6.5). Converting it to process colour would make it paint.
func paintsNothing(ctx context.Context, doc *document.Document, sp imageSpace) bool {
	if len(sp.arr) < 2 {
		return false
	}
	switch sp.family {
	case familySeparation:
		name, err := document.ResolveName(ctx, doc, sp.arr[1])
		return err == nil && name == "None"
	case familyDeviceN:
		names, err := document.ResolveArray(ctx, doc, sp.arr[1])
		if err != nil || len(names) == 0 {
			return false
		}
		for _, n := range names {
			if name, err := document.ResolveName(ctx, doc, n); err != nil || name != "None" {
				return false
			}
		}
		return true
	}
	return false
}

// inTarget reports whether an image in sp is already in the target space:
// the same device family, with no default colour space in resources standing
// in for it (ISO 32000-1, 8.6.5.6), or the same ICC profile object.
func (im *Images) inTarget(ctx context.Context, sp imageSpace, resources ast.PdfDict) bool {
	if name, ok := im.cfg.Space.(ast.PdfName); ok {
		if sp.arr != nil || sp.family != name {
			return false
		}
		return !hasDefault(ctx, im.doc, resources, name)
	}
	got, err := document.ResolveObject(ctx, im.doc, sp.obj)
	if err != nil {
		return false
	}
	want, err := document.ResolveObject(ctx, im.doc, im.cfg.Space)
	if err != nil {
		return false
	}
	return fmt.Sprint(got) == fmt.Sprint(want)
}

// hasDefault reports whether resources hold the default colour space of the
// device family (ISO 32000-1, 8.6.5.6).
func hasDefault(ctx context.Context, doc *document.Document, resources ast.PdfDict, family ast.PdfName) bool {
	if resources == nil {
		return false
	}
	spaces, err := document.ResolveDict(ctx, doc, resources["ColorSpace"])
	if err != nil || spaces == nil {
		return false
	}
	_, ok := spaces[ast.PdfName("Default"+strings.TrimPrefix(string(family), "Device"))]
	return ok
}

// defaultsKey spells out the default colour spaces of resources, which a
// conversion of a device space depends on.
func defaultsKey(ctx context.Context, doc *document.Document, resources ast.PdfDict) string {
	if resources == nil {
		return ""
	}
	spaces, err := document.ResolveDict(ctx, doc, resources["ColorSpace"])
	if err != nil || spaces == nil {
		return ""
	}
	var sb strings.Builder
	for _, name := range []ast.PdfName{"DefaultGray", "DefaultRGB", "DefaultCMYK"} {
		if def, ok := spaces[name]; ok {
			fmt.Fprintf(&sb, "|%s=%v", name, def)
		}
	}
	return sb.String()
}

// lastFilter returns the last filter of an image dictionary, whose output
// the samples are (ISO 32000-1, 7.4.1), or "" for none.
func lastFilter(ctx context.Context, doc *document.Document, dict ast.PdfDict) ast.PdfName {
	obj, err := document.ResolveObject(ctx, doc, dict["Filter"])
	if err != nil {
		return ""
	}
	switch f := obj.(type) {
	case ast.PdfName:
		return f
	case ast.PdfArray:
		if len(f) == 0 {
			return ""
		}
		name, err := document.ResolveName(ctx, doc, f[len(f)-1])
		if err != nil {
			return ""
		}
		return name
	}
	return ""
}

// imageIntent returns the rendering intent an image is converted under: its
// own /Intent (ISO 32000-1, 8.9.5, Table 89), read as the image decoder
// reads it ([pdfimage.ImageIntent]), else the configured one.
func (im *Images) imageIntent(ctx context.Context, dict ast.PdfDict) ast.PdfName {
	if intent, own := pdfimage.ImageIntent(ctx, im.doc, dict); own {
		return ast.PdfName(intent.String())
	}
	return im.cfg.Intent
}

// srgbSpace is the space of the sRGB pixels the image decoder returns, an
// ICC profile of IEC 61966-2-1.
func srgbSpace() ast.PdfObject {
	return ast.PdfArray{familyICCBased, ast.NewStream(ast.PdfDict{"N": ast.PdfInt(3)}, icc.SRGBProfile())}
}
