package image

import (
	"context"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// CMYKProfile returns the ICC profile DeviceCMYK colours on page are read
// through, the one [cmykTransform] reads them through: the first CMYK
// destination profile among the page's output intents, else among the
// document's, else the profile the caller supplied, else litho's stock
// printer profile, which reads as its built-in profile does and also holds
// the tables back from the connection space ([icc.CMYKPrinterProfile]). page
// may be nil. It returns nil only when that last profile fails to parse.
func CMYKProfile(ctx context.Context, doc *document.Document, page ast.PdfDict) *icc.Profile {
	if p := outputIntentsCMYK(ctx, doc, page["OutputIntents"]); p != nil {
		return p
	}
	if catalog, err := document.ResolveCatalog(ctx, doc); err != nil {
		doc.Logger.Debug("resolving catalog for output intents", slog.Any("err", err))
	} else if p := outputIntentsCMYK(ctx, doc, catalog.Dict["OutputIntents"]); p != nil {
		return p
	}
	if doc.CMYKProfile != nil {
		p, err := icc.Parse(doc.CMYKProfile)
		if err == nil && p.Components() == 4 {
			return p
		}
		doc.Logger.Debug("reading the caller's CMYK profile", slog.Any("err", err))
	}
	p, err := icc.Parse(icc.CMYKPrinterProfile())
	if err != nil {
		doc.Logger.Debug("reading litho's CMYK printer profile", slog.Any("err", err))
		return nil
	}
	return p
}

// outputIntentsCMYK returns the destination profile of the first output
// intent of the /OutputIntents array obj that describes four components, or
// nil when none does.
func outputIntentsCMYK(ctx context.Context, doc *document.Document, obj ast.PdfObject) *icc.Profile {
	if obj == nil {
		return nil
	}
	intents, err := document.ResolveArray(ctx, doc, obj)
	if err != nil {
		doc.Logger.Debug("resolving /OutputIntents", slog.Any("err", err))
		return nil
	}
	for _, e := range intents {
		dict, err := document.ResolveDict(ctx, doc, e)
		if err != nil || dict == nil {
			continue
		}
		profile, ok := dict["DestOutputProfile"]
		if !ok {
			continue
		}
		p, err := readProfile(ctx, doc, profile)
		if err != nil {
			doc.Logger.Debug("reading an output intent's profile", slog.Any("err", err))
			continue
		}
		if p.Components() == 4 {
			return p
		}
	}
	return nil
}

// ICCBasedProfile returns the profile of the ICCBased colour space csObj, an
// array [/ICCBased stream] (ISO 32000-1, 8.6.5.5), or nil when csObj is no
// such array or its profile does not read.
func ICCBasedProfile(ctx context.Context, doc *document.Document, csObj ast.PdfObject) *icc.Profile {
	arr, err := document.ResolveArray(ctx, doc, csObj)
	if err != nil || len(arr) < 2 {
		return nil
	}
	if family, err := document.ResolveName(ctx, doc, arr[0]); err != nil || family != "ICCBased" {
		return nil
	}
	p, err := readProfile(ctx, doc, arr[1])
	if err != nil {
		doc.Logger.Debug("reading an ICCBased profile", slog.Any("err", err))
		return nil
	}
	return p
}

// OutputIntentsFamily returns the device colour space family the destination
// profile of the first output intent of the /OutputIntents array obj
// characterises (ISO 32000-1, 14.11.5): "DeviceGray", "DeviceRGB" or
// "DeviceCMYK" for a profile of 1, 3 or 4 components. An intent with no
// destination profile, or one that does not read, is passed over. It returns
// "" when no intent has such a profile, or obj is no array.
func OutputIntentsFamily(ctx context.Context, doc *document.Document, obj ast.PdfObject) string {
	if obj == nil {
		return ""
	}
	intents, err := document.ResolveArray(ctx, doc, obj)
	if err != nil {
		doc.Logger.Debug("resolving /OutputIntents", slog.Any("err", err))
		return ""
	}
	for _, e := range intents {
		dict, err := document.ResolveDict(ctx, doc, e)
		if err != nil || dict == nil {
			continue
		}
		profile, ok := dict["DestOutputProfile"]
		if !ok {
			continue
		}
		p, err := readProfile(ctx, doc, profile)
		if err != nil {
			doc.Logger.Debug("reading an output intent's profile", slog.Any("err", err))
			continue
		}
		switch p.Components() {
		case 1:
			return "DeviceGray"
		case 3:
			return "DeviceRGB"
		case 4:
			return "DeviceCMYK"
		}
	}
	return ""
}
