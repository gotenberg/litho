package image

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// maxICCProfileBytes bounds the decoded bytes of an ICC profile stream. It is
// an anti-DoS guard, not spec-mandated: the densest lookup table in common
// use, 33 points over four inputs at 16 bits, makes a profile of about 7 MB,
// so 16 MB leaves room while a crafted stream cannot demand more.
const maxICCProfileBytes = 16 << 20

// deviceCMYKKey, deviceRGBKey and deviceGrayKey key the colour cache entries
// for the profiles the device spaces are read in, whichever of the document,
// the caller or litho supplies them. Object numbers are positive, so they
// cannot collide with a profile stream's.
const (
	deviceCMYKKey = -1
	deviceRGBKey  = -2
	deviceGrayKey = -3
)

// colorContext holds what the colours of an image depend on beyond its own
// dictionary. The zero value, for a soft mask or a caller with no page,
// substitutes nothing and leaves output intents to the document.
type colorContext struct {
	// resources is the resource dictionary of the content stream that paints
	// the image. Its DefaultGray, DefaultRGB and DefaultCMYK entries stand in
	// for the device spaces (ISO 32000-1, 8.6.5.6), and a form, a pattern or
	// an annotation appearance is covered by its own resources alone
	// (ISO 32000-2, 8.6.5.7).
	resources ast.PdfDict
	// page is the page dictionary, whose /OutputIntents replace the
	// document's for that page (ISO 32000-2, 14.11.5).
	page ast.PdfDict
	// dataMask is the soft mask the image's JPEG 2000 data carries
	// (ISO 32000-1, Table 89 /SMaskInData) when it is applied: its colour
	// channels are unpremultiplied while they still hold the data's values,
	// before any colour conversion, when the mask marks them premultiplied.
	dataMask *DataSoftMask
	// bpc is the /UseBlackPtComp of the graphics state the image is painted
	// under (ISO 32000-2, Table 57): every colour it converts carries it
	// ([colorContext.renderingIntent]).
	bpc icc.BPC
	// stateIntent is the rendering intent of the graphics state the image
	// is painted under, set by ri or an ExtGState /RI, when painted is true.
	// An image that states no /Intent of its own is converted with it (ISO
	// 32000-1, 8.6.5.8). A caller with no graphics state leaves painted
	// false, and such an image takes relative colorimetric, the graphics
	// state's initial intent.
	stateIntent icc.Intent
	painted     bool
}

// effectiveSpaces returns dict with its /ColorSpace replaced by the space its
// colours are read in: a default colour space where one stands in for a
// device space ([colorContext.withDefaults]), then an /Alternate where it
// stands in for an ICCBased space whose profile litho cannot use
// ([withICCAlternates]). dict is not modified.
func (cc colorContext) effectiveSpaces(ctx context.Context, doc *document.Document, dict ast.PdfDict) ast.PdfDict {
	dict = cc.withDefaults(ctx, doc, dict)
	return withICCAlternates(ctx, doc, dict, cc.renderingIntent(ctx, doc, dict))
}

// withDefaults returns dict with its /ColorSpace replaced where a default
// colour space stands in for a device space: the space itself, the base of an
// Indexed space, or the alternate of a Separation or DeviceN space. A device
// space in those two positions is selected like any other, and litho's PDF/A
// validation, like veraPDF, counts it as device colour the defaults cover.
// dict is not modified.
func (cc colorContext) withDefaults(ctx context.Context, doc *document.Document, dict ast.PdfDict) ast.PdfDict {
	if cc.resources == nil {
		return dict
	}
	csObj, ok := dict["ColorSpace"]
	if !ok {
		return dict
	}
	resolved, err := document.ResolveObject(ctx, doc, csObj)
	if err != nil {
		doc.Logger.Debug("resolving image colour space for default colour spaces", slog.Any("err", err))
		return dict
	}
	var effective ast.PdfObject
	switch v := resolved.(type) {
	case ast.PdfName:
		def, ok := cc.deviceDefault(ctx, doc, v)
		if !ok {
			return dict
		}
		effective = def
	case ast.PdfArray:
		if len(v) < 2 {
			return dict
		}
		family, err := document.ResolveName(ctx, doc, v[0])
		if err != nil {
			return dict
		}
		pos := map[ast.PdfName]int{"Indexed": 1, "Separation": 2, "DeviceN": 2}[family]
		if pos == 0 || pos >= len(v) {
			return dict
		}
		base, err := document.ResolveObject(ctx, doc, v[pos])
		if err != nil {
			return dict
		}
		name, isName := base.(ast.PdfName)
		if !isName {
			return dict
		}
		def, ok := cc.deviceDefault(ctx, doc, name)
		if !ok {
			return dict
		}
		arr := slices.Clone(v)
		arr[pos] = def
		effective = arr
	default:
		return dict
	}
	out := maps.Clone(dict)
	out["ColorSpace"] = effective
	return out
}

// deviceDefault returns the default colour space cc's resources name for a
// device space. ISO 32000-1, 8.6.5.6 requires a CIE-based space with the
// device space's number of components: an ICCBased space of that /N, or
// CalGray for DeviceGray and CalRGB for DeviceRGB. Any other value is ignored
// and the device space kept.
func (cc colorContext) deviceDefault(ctx context.Context, doc *document.Document, device ast.PdfName) (ast.PdfObject, bool) {
	key, ok := map[ast.PdfName]ast.PdfName{
		"DeviceGray": "DefaultGray", "DeviceRGB": "DefaultRGB", "DeviceCMYK": "DefaultCMYK",
	}[device]
	if !ok {
		return nil, false
	}
	spaces, err := document.ResolveDict(ctx, doc, cc.resources["ColorSpace"])
	if err != nil || spaces == nil {
		return nil, false
	}
	defObj, ok := spaces[key]
	if !ok {
		return nil, false
	}
	arr, err := document.ResolveArray(ctx, doc, defObj)
	if err != nil || len(arr) < 2 {
		doc.Logger.Debug("ignoring a default colour space that is no colour space array",
			slog.String("key", string(key)), slog.Any("err", err))
		return nil, false
	}
	family, err := document.ResolveName(ctx, doc, arr[0])
	if err != nil {
		return nil, false
	}
	switch {
	case family == "ICCBased" && iccBasedSpace(ctx, doc, arr) == string(device),
		family == "CalGray" && device == "DeviceGray",
		family == "CalRGB" && device == "DeviceRGB":
		return arr, true
	}
	doc.Logger.Debug("ignoring a default colour space of another kind",
		slog.String("key", string(key)), slog.String("family", string(family)))
	return nil, false
}

// renderingIntent returns the rendering intent an image dictionary's colours
// are converted with, carrying cc's black point compensation choice. Per ISO
// 32000-1, 8.6.5.8 the image's /Intent overrides the current rendering
// intent, so the graphics state's intent applies only to an image that
// states none, and an unrecognised name selects relative colorimetric.
func (cc colorContext) renderingIntent(ctx context.Context, doc *document.Document, dict ast.PdfDict) icc.Intent {
	intent, own := ImageIntent(ctx, doc, dict)
	if !own && cc.painted {
		intent = cc.stateIntent
	}
	return intent.WithBPC(cc.bpc)
}

// renderingIntent returns the rendering intent of an image dictionary's
// /Intent, with no black point compensation choice: relative colorimetric,
// the graphics state's initial intent (ISO 32000-1, 8.6.5.8), for an image
// that states none, as for a caller with no graphics state.
func renderingIntent(ctx context.Context, doc *document.Document, dict ast.PdfDict) icc.Intent {
	intent, _ := ImageIntent(ctx, doc, dict) // an image with no /Intent reads as relative colorimetric
	return intent
}

// ImageIntent returns the rendering intent an image dictionary's /Intent
// names, and whether it names one. An entry whose value is null, or a
// reference to no object, is absent (ISO 32000-1, 7.3.7 and 7.3.10). Any
// other value that is not a name Table 70 defines selects relative
// colorimetric, as 8.6.5.8 makes an unrecognised name do, and still counts as
// the image's own. The renderer's blending and recolouring read an image's
// intent through it, as the decoder does.
func ImageIntent(ctx context.Context, doc *document.Document, dict ast.PdfDict) (icc.Intent, bool) {
	obj, ok := dict["Intent"]
	if !ok {
		return icc.RelativeColorimetric, false
	}
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		doc.Logger.Debug("resolving image /Intent", slog.Any("err", err))
		return icc.RelativeColorimetric, false
	}
	switch v := resolved.(type) {
	case nil, ast.PdfNull:
		return icc.RelativeColorimetric, false
	case ast.PdfName:
		return icc.ParseIntent(string(v)), true
	}
	doc.Logger.Debug("image /Intent is not a name", slog.String("type", fmt.Sprintf("%T", resolved)))
	return icc.RelativeColorimetric, true
}

// colorTransform returns the transform that takes samples of a colour space
// to sRGB, or nil when they are read as device values. csObj is the colour
// space object, space the device space litho reads the samples in, and
// components their count.
//
// An ICCBased space is read through its profile (ISO 32000-1, 8.6.5.5) when
// the profile describes as many components as the samples hold. When it
// cannot be used, as when a JPEG's codestream holds another count, the space
// falls back to the device space its /N names. A profile unusable whatever
// the samples hold has already given way to its /Alternate, when it has one
// ([withICCAlternates]). DeviceCMYK is read through
// [cmykTransform], and DeviceRGB and DeviceGray through [deviceTransform].
// The CIE-based spaces keep their own readings, even where litho reads their
// samples in the device space of their component count.
func colorTransform(ctx context.Context, doc *document.Document, csObj ast.PdfObject, space string, components int, intent icc.Intent, cc colorContext) *icc.Transform {
	if csObj != nil {
		resolved, err := document.ResolveObject(ctx, doc, csObj)
		if err != nil {
			doc.Logger.Debug("resolving colour space for colour management", slog.Any("err", err))
		}
		if arr, ok := resolved.(ast.PdfArray); ok && len(arr) >= 2 {
			family, err := document.ResolveName(ctx, doc, arr[0])
			if err != nil {
				doc.Logger.Debug("resolving colour space family", slog.Any("err", err))
			}
			if family == "ICCBased" {
				if t := profileTransform(ctx, doc, arr[1], components, intent); t != nil {
					return t
				}
			}
		}
	}
	if space == "DeviceCMYK" && components == 4 {
		return cmykTransform(ctx, doc, intent, cc)
	}
	if (space == "DeviceRGB" && components == 3 || space == "DeviceGray" && components == 1) &&
		denotesDevice(ctx, doc, csObj) {
		return deviceTransform(ctx, doc, space, components, intent, cc)
	}
	return nil
}

// denotesDevice reports whether the colour space object csObj leaves its
// samples to a device space: it is the name of one, or an ICCBased space
// whose profile could not be used, which falls back to the device space of
// its component count (ISO 32000-1, 8.6.5.5). A CalRGB, CalGray or Lab space,
// which litho also reads in a device space of its component count, is
// CIE-based and characterised by its own dictionary, and a missing space, as
// a JPEG 2000 image may have, takes its colours from the image's own data.
func denotesDevice(ctx context.Context, doc *document.Document, csObj ast.PdfObject) bool {
	if csObj == nil {
		return false
	}
	resolved, err := document.ResolveObject(ctx, doc, csObj)
	if err != nil {
		doc.Logger.Debug("resolving colour space for output intents", slog.Any("err", err))
		return false
	}
	switch v := resolved.(type) {
	case ast.PdfName:
		return v == "DeviceRGB" || v == "DeviceGray"
	case ast.PdfArray:
		if len(v) == 0 {
			return false
		}
		family, err := document.ResolveName(ctx, doc, v[0])
		return err == nil && family == "ICCBased"
	}
	return false
}

// profileTransform returns the transform of the ICC profile stream obj for
// the intent, or nil when the profile cannot be read, cannot be transformed,
// or describes other than components components. Transforms are cached per
// profile object, failures included, so a document's images share one.
func profileTransform(ctx context.Context, doc *document.Document, obj ast.PdfObject, components int, intent icc.Intent) *icc.Transform {
	matching := func(t *icc.Transform) *icc.Transform {
		if t == nil || t.Components() != components {
			return nil
		}
		return t
	}
	ref, isRef := obj.(ast.PdfIndirectRef)
	if isRef {
		if v, ok := doc.CachedColorTransform(ref.ObjectNum, int(intent)); ok {
			t, _ := v.(*icc.Transform) // a nil entry records a profile that could not be used
			return matching(t)
		}
	}
	t, err := readProfileTransform(ctx, doc, obj, intent)
	if err != nil {
		doc.Logger.Debug("reading ICC profile", slog.Any("err", err))
	}
	if isRef {
		size := 0
		if t != nil {
			size = t.Size()
		}
		doc.SetCachedColorTransform(ref.ObjectNum, int(intent), t, size)
	}
	return matching(t)
}

// readProfileTransform reads the ICC profile stream obj and builds its
// transform for the intent.
func readProfileTransform(ctx context.Context, doc *document.Document, obj ast.PdfObject, intent icc.Intent) (*icc.Transform, error) {
	p, err := readProfile(ctx, doc, obj)
	if err != nil {
		return nil, err
	}
	return p.NewTransform(intent)
}

// readProfile reads and parses the ICC profile stream obj.
func readProfile(ctx context.Context, doc *document.Document, obj ast.PdfObject) (*icc.Profile, error) {
	stream, err := document.ResolveStream(ctx, doc, obj)
	if err != nil {
		return nil, fmt.Errorf("resolving ICC profile stream: %w", err)
	}
	if stream.Dict == nil {
		return nil, fmt.Errorf("ICC profile object %v is not a stream", obj)
	}
	rc, err := doc.NewStreamReader(ctx, stream)
	if err != nil {
		return nil, fmt.Errorf("opening ICC profile stream: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(rc, maxICCProfileBytes+1))
	if closeErr := rc.Close(); closeErr != nil {
		doc.Logger.Debug("closing ICC profile stream", slog.Any("err", closeErr))
	}
	if err != nil {
		return nil, fmt.Errorf("reading ICC profile stream: %w", err)
	}
	if len(data) > maxICCProfileBytes {
		return nil, fmt.Errorf("ICC profile exceeds the %d-byte safety cap", maxICCProfileBytes)
	}
	return icc.Parse(data)
}

// embeddedTransform returns the transform of an ICC profile an image's own
// data carries, as a JPEG 2000 colour specification does, or nil when there
// is none or it cannot be used for components components. It is built per
// image: the profile has no object to key a cache on, and images that carry
// one are rare.
func embeddedTransform(doc *document.Document, profile []byte, components int, intent icc.Intent) *icc.Transform {
	if profile == nil {
		return nil
	}
	if len(profile) > maxICCProfileBytes {
		doc.Logger.Debug("ignoring an embedded ICC profile past the safety cap", slog.Int("bytes", len(profile)))
		return nil
	}
	p, err := icc.Parse(profile)
	var t *icc.Transform
	if err == nil {
		t, err = p.NewTransform(intent)
	}
	if err != nil {
		doc.Logger.Debug("reading an embedded ICC profile", slog.Any("err", err))
		return nil
	}
	if t.Components() != components {
		doc.Logger.Debug("ignoring an embedded ICC profile of another component count",
			slog.Int("profile", t.Components()), slog.Int("samples", components))
		return nil
	}
	return t
}

// cmykTransform returns the transform DeviceCMYK samples are read through:
// the CMYK destination profile of the page's output intents, else of the
// document's, else the profile the caller supplied, else litho's built-in
// profile. Nil, which leaves the samples to the device formula, arises only
// when the built-in profile itself fails.
//
// ISO 32000-1, 14.11.5 has an output intent's /DestOutputProfile
// characterise the device a document was prepared for, and ISO 19005-2,
// 6.2.4.3 admits DeviceCMYK into a PDF/A file on the strength of an output
// intent with a CMYK destination profile, which is then what those colours
// mean. ISO 32000-2, 14.11.5 lets a page carry output intents of its own,
// which replace the document's for that page. A document without one leaves
// DeviceCMYK uncharacterised, and the naive formula (1-c)(1-k) then paints
// cyan as (0, 255, 255) where a press prints (0, 174, 239).
func cmykTransform(ctx context.Context, doc *document.Document, intent icc.Intent, cc colorContext) *icc.Transform {
	return outputIntentTransform(ctx, doc, deviceCMYKKey, 4, intent, cc, func() *icc.Transform {
		var t *icc.Transform
		if doc.CMYKProfile != nil {
			p, err := icc.Parse(doc.CMYKProfile)
			if err == nil {
				t, err = p.NewTransform(intent)
			}
			if err != nil {
				doc.Logger.Debug("reading the caller's CMYK profile", slog.Any("err", err))
			}
			if t != nil && t.Components() != 4 {
				t = nil
			}
		}
		if t == nil {
			var err error
			if t, err = icc.BuiltinCMYK(intent); err != nil {
				doc.Logger.Debug("reading the built-in CMYK profile", slog.Any("err", err))
			}
		}
		return t
	})
}

// deviceTransform returns the transform DeviceRGB or DeviceGray samples, of
// components components, are read through: the destination profile of that
// many components among the page's output intents, else among the
// document's. Nil, which leaves the samples as sRGB values, means neither
// states one.
//
// The reading mirrors [cmykTransform]'s. ISO 32000-1, 14.11.5 has an output
// intent's /DestOutputProfile characterise the device a document was
// prepared for, and ISO 19005-2, 6.2.4.3 admits DeviceRGB into a PDF/A file
// on the strength of an output intent with an RGB destination profile, so an
// RGB destination profile is what those colours mean, and a Gray one what
// DeviceGray colours mean. A destination profile of another family leaves
// the space uncharacterised. A DefaultRGB or DefaultGray space (ISO 32000-1,
// 8.6.5.6) has already replaced the device space by then (see
// [colorContext.withDefaults]), so it takes precedence. MuPDF reads the
// device spaces through the document's first output intent the same way,
// a default colour space first.
//
// A destination profile that gives every colour back as it came, as the sRGB
// profile most PDF/A files carry does, is dropped: the samples already are
// sRGB values, and converting them would cost a transform per pixel to round
// each one back.
func deviceTransform(ctx context.Context, doc *document.Document, space string, components int, intent icc.Intent, cc colorContext) *icc.Transform {
	key := deviceRGBKey
	if space == "DeviceGray" {
		key = deviceGrayKey
	}
	t := outputIntentTransform(ctx, doc, key, components, intent, cc, func() *icc.Transform { return nil })
	if t != nil && t.Identity() {
		return nil
	}
	return t
}

// outputIntentTransform returns the transform the device space keyed key, of
// components components, is read through: the destination profile of that
// many components among the page's output intents, else among the
// document's, else what fallback returns. The document-wide answer is cached
// under key.
func outputIntentTransform(ctx context.Context, doc *document.Document, key, components int, intent icc.Intent, cc colorContext, fallback func() *icc.Transform) *icc.Transform {
	if obj, ok := cc.page["OutputIntents"]; ok {
		// Not kept under key, which answers for every page: the profile's own
		// cache entry makes the next image on the page cheap.
		if t := outputIntentsProfile(ctx, doc, obj, components, intent); t != nil {
			return t
		}
	}
	if v, ok := doc.CachedColorTransform(key, int(intent)); ok {
		t, _ := v.(*icc.Transform) // a nil entry records that no profile could be used
		return t
	}
	var t *icc.Transform
	if catalog, err := document.ResolveCatalog(ctx, doc); err != nil {
		doc.Logger.Debug("resolving catalog for output intents", slog.Any("err", err))
	} else {
		t = outputIntentsProfile(ctx, doc, catalog.Dict["OutputIntents"], components, intent)
	}
	if t == nil {
		t = fallback()
	}
	// The entry aliases a transform the cache already counts, one the caller
	// supplied, or the built-in one litho shares, so it adds no bytes.
	doc.SetCachedColorTransform(key, int(intent), t, 0)
	return t
}

// outputIntentsProfile returns the transform of the first output intent of
// the /OutputIntents array obj whose destination profile describes
// components components.
func outputIntentsProfile(ctx context.Context, doc *document.Document, obj ast.PdfObject, components int, intent icc.Intent) *icc.Transform {
	intents, err := document.ResolveArray(ctx, doc, obj)
	if err != nil {
		doc.Logger.Debug("resolving /OutputIntents", slog.Any("err", err))
		return nil
	}
	for _, obj := range intents {
		dict, err := document.ResolveDict(ctx, doc, obj)
		if err != nil || dict == nil {
			doc.Logger.Debug("resolving output intent", slog.Any("err", err))
			continue
		}
		profile, ok := dict["DestOutputProfile"]
		if !ok {
			continue
		}
		if t := profileTransform(ctx, doc, profile, components, intent); t != nil {
			return t
		}
	}
	return nil
}

// colorCacheBits sizes the table a [converter] remembers colours in: 4096
// slots of 11 bytes, small beside any picture worth converting.
const colorCacheBits = 12

// converter converts colours through a transform, remembering the colour last
// converted in each slot of a small hash table. A picture repeats its
// colours, and the lookup costs a fraction of the conversion.
type converter struct {
	t    *icc.Transform
	keys []uint64
	rgb  [][3]uint8
}

// newConverter returns a converter over t, or nil when t is nil.
func newConverter(t *icc.Transform) *converter {
	if t == nil {
		return nil
	}
	return &converter{t: t}
}

// convert converts one colour of t.Components() bytes to sRGB.
func (c *converter) convert(in []byte) (uint8, uint8, uint8) {
	if c.keys == nil {
		c.keys = make([]uint64, 1<<colorCacheBits)
		c.rgb = make([][3]uint8, 1<<colorCacheBits)
	}
	// A key packs up to four bytes, with bit 32 set so the zero slot of an
	// empty table never matches.
	key := uint64(1) << 32
	for i, v := range in {
		key |= uint64(v) << (8 * i)
	}
	slot := (key * 0x9E3779B97F4A7C15) >> (64 - colorCacheBits)
	if c.keys[slot] == key {
		v := c.rgb[slot]
		return v[0], v[1], v[2]
	}
	r, g, b := c.t.Convert(in)
	c.keys[slot], c.rgb[slot] = key, [3]uint8{r, g, b}
	return r, g, b
}

// convertTable converts a colour table of entries of t.Components() bytes
// into one of three-byte sRGB entries. A trailing partial entry is dropped.
func (c *converter) convertTable(table []byte) []byte {
	n := c.t.Components()
	out := make([]byte, 0, len(table)/n*3)
	for i := 0; i+n <= len(table); i += n {
		r, g, b := c.convert(table[i : i+n])
		out = append(out, r, g, b)
	}
	return out
}
