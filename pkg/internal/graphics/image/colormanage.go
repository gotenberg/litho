package image

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// maxICCProfileBytes bounds the decoded bytes of an ICC profile stream. It is
// an anti-DoS guard, not spec-mandated: the densest lookup table in common
// use, 33 points over four inputs at 16 bits, makes a profile of about 7 MB,
// so 16 MB leaves room while a crafted stream cannot demand more.
const maxICCProfileBytes = 16 << 20

// deviceCMYKKey keys the colour cache entry for the profile DeviceCMYK is
// read in, whichever of the document, the caller or litho supplies it. Object
// numbers are positive, so it cannot collide with a profile stream's.
const deviceCMYKKey = -1

// renderingIntent returns the rendering intent of an image dictionary's
// /Intent. Per ISO 32000-1, 8.6.5.8 an unrecognised name selects relative
// colorimetric, which is also the graphics state's initial intent, and so the
// reading of an image that states none.
func renderingIntent(ctx context.Context, doc *document.Document, dict ast.PdfDict) icc.Intent {
	obj, ok := dict["Intent"]
	if !ok {
		return icc.RelativeColorimetric
	}
	name, err := document.ResolveName(ctx, doc, obj)
	if err != nil {
		doc.Logger.Debug("resolving image /Intent", slog.Any("err", err))
		return icc.RelativeColorimetric
	}
	switch name {
	case "Perceptual":
		return icc.Perceptual
	case "Saturation":
		return icc.Saturation
	case "AbsoluteColorimetric":
		return icc.AbsoluteColorimetric
	}
	return icc.RelativeColorimetric
}

// colorTransform returns the transform that takes samples of a colour space
// to sRGB, or nil when they are read as device values. csObj is the colour
// space object, space the device space litho reads the samples in, and
// components their count.
//
// An ICCBased space is read through its profile (ISO 32000-1, 8.6.5.5) when
// the profile describes as many components as the samples hold. When it
// cannot be used, as when a JPEG's codestream holds another count, the space
// falls back to the device space its /N names. DeviceCMYK is read through
// [cmykTransform]. DeviceGray, DeviceRGB and the CIE-based spaces keep their
// own readings.
func colorTransform(ctx context.Context, doc *document.Document, csObj ast.PdfObject, space string, components int, intent icc.Intent) *icc.Transform {
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
		return cmykTransform(ctx, doc, intent)
	}
	return nil
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
	p, err := icc.Parse(data)
	if err != nil {
		return nil, err
	}
	return p.NewTransform(intent)
}

// cmykTransform returns the transform DeviceCMYK samples are read through:
// the CMYK destination profile of the document's output intent, else the
// profile the caller supplied, else litho's built-in profile. Nil, which
// leaves the samples to the device formula, arises only when the built-in
// profile itself fails.
//
// ISO 32000-1, 14.11.5 has an output intent's /DestOutputProfile
// characterise the device a document was prepared for, and ISO 19005-2,
// 6.2.4.3 admits DeviceCMYK into a PDF/A file on the strength of an output
// intent with a CMYK destination profile, which is then what those colours
// mean. A document without one leaves DeviceCMYK uncharacterised, and the
// naive formula (1-c)(1-k) then paints cyan as (0, 255, 255) where a press
// prints (0, 174, 239).
func cmykTransform(ctx context.Context, doc *document.Document, intent icc.Intent) *icc.Transform {
	if v, ok := doc.CachedColorTransform(deviceCMYKKey, int(intent)); ok {
		t, _ := v.(*icc.Transform) // a nil entry records that even the built-in profile failed
		return t
	}
	t := outputIntentCMYK(ctx, doc, intent)
	if t == nil && doc.CMYKProfile != nil {
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
	// The entry aliases a transform the cache already counts, one the caller
	// supplied, or the built-in one litho shares, so it adds no bytes.
	doc.SetCachedColorTransform(deviceCMYKKey, int(intent), t, 0)
	return t
}

// outputIntentCMYK returns the transform of the first output intent in the
// catalog's /OutputIntents whose destination profile is a CMYK profile.
func outputIntentCMYK(ctx context.Context, doc *document.Document, intent icc.Intent) *icc.Transform {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		doc.Logger.Debug("resolving catalog for output intents", slog.Any("err", err))
		return nil
	}
	intents, err := document.ResolveArray(ctx, doc, catalog.Dict["OutputIntents"])
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
		if t := profileTransform(ctx, doc, profile, 4, intent); t != nil {
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
