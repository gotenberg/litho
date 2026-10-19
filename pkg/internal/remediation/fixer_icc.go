package remediation

import (
	"context"
	"fmt"
	"log/slog"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

func matchCorruptICC(v compliance.Violation) bool {
	// Clause 6.2.3/6.2.2/6.5.3: OutputIntent ICC profile validation failures.
	return clausePrefix(v, "6.2.3", "6.2.2", "6.5.3") && descriptionContains(v, "DestOutputProfile")
}

func fixCorruptICC(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}

	oiObj, ok := catalogDict["OutputIntents"]
	if !ok {
		return nil, nil
	}
	oiArr, err := document.ResolveArray(ctx, doc, oiObj)
	if err != nil || len(oiArr) == 0 {
		return nil, nil //nolint:nilerr // An unresolvable /OutputIntents is null (ISO 32000-1, 7.3.10): no profile to repair.
	}

	var fixes []string
	newOIArr := make(ast.PdfArray, len(oiArr))
	converted := make(map[int]bool)

	for i, oiEntry := range oiArr {
		var oiRef ast.PdfIndirectRef
		if ref, ok := oiEntry.(ast.PdfIndirectRef); ok {
			oiRef = ref
		}

		oiDict, err := document.ResolveDict(ctx, doc, oiEntry)
		if err != nil || oiDict == nil {
			doc.Logger.Debug("resolving OutputIntent dict failed",
				slog.Int("index", i),
				slog.Any("err", err),
			)
			newOIArr[i] = oiEntry
			continue
		}

		// Check if DestOutputProfile exists and is valid.
		dpObj, ok := oiDict["DestOutputProfile"]
		if !ok {
			// Missing DestOutputProfile is a spec violation; inject a
			// stock sRGB profile so the OutputIntent becomes valid.
			// OutputConditionIdentifier is required too, fill it in
			// when absent.
			if _, hasOCI := oiDict["OutputConditionIdentifier"]; !hasOCI {
				newOI := make(ast.PdfDict, len(oiDict)+1)
				maps.Copy(newOI, oiDict)
				newOI["OutputConditionIdentifier"] = ast.PdfString("sRGB IEC61966-2.1")
				oiDict = newOI
			}
			// No profile to inspect, so the destination colour space is
			// unknown and sRGB is the only defensible stock choice. The
			// identifier filled in just above says sRGB too.
			replaceICCProfile(alloc, oiDict, oiRef, nil, "")
			fixes = append(fixes, fmt.Sprintf("added DestOutputProfile to OutputIntent[%d]", i))
			newOIArr[i] = oiEntry
			continue
		}

		stream, err := document.ResolveStream(ctx, doc, dpObj)
		if err != nil {
			doc.Logger.Debug("resolving DestOutputProfile stream failed",
				slog.Int("index", i),
				slog.Any("err", err),
			)
			newOIArr[i] = oiEntry
			continue
		}

		// The header fields below are read at fixed offsets into the profile,
		// so the profile has to be decoded first: ast.PdfStream.Bytes returns
		// the body before filter decoding, and a FlateDecode profile put
		// deflate output at every offset. Each field then failed its test and a
		// valid profile was replaced with litho's own sRGB copy.
		data, err := doc.DecodeStream(ctx, stream)
		if err != nil || len(data) < 128 {
			// Corrupt or too short. Replace with a valid profile.
			newOIArr[i] = oiEntry
			// The profile did not decode, or is too short to carry a header,
			// so byte 16 onward cannot be trusted to name a colour space.
			// sRGB is the only safe stock replacement here.
			replaceICCProfile(alloc, oiDict, oiRef, dpObj, "")
			fixes = append(fixes, fmt.Sprintf("replaced corrupt ICC profile in OutputIntent[%d]", i))
			continue
		}

		// Validate ICC header: version major (byte 8) must be 2 or 4, device
		// class must be "prtr" or "mntr", color space must be "GRAY", "RGB ",
		// or "CMYK". The validator rejects ICC version 5+ (it is not admitted
		// by ISO 15076-1:2005), so the fixer must replace such a profile too;
		// otherwise a valid-class v5 profile is flagged but never repaired.
		version := data[8]
		deviceClass := string(data[12:16])
		colorSpace := string(data[16:20])

		validVersion := version == 2 || version == 4
		validClass := deviceClass == "prtr" || deviceClass == "mntr"
		validCS := colorSpace == "GRAY" || colorSpace == "RGB " || colorSpace == "CMYK"

		if !validVersion || !validClass || !validCS {
			// The header decoded, so colorSpace is known and the replacement
			// keeps it: a CMYK profile rejected for its version is replaced
			// with a CMYK profile, not silently turned into RGB. A colour
			// space that is itself invalid falls through to sRGB.
			replaceICCProfile(alloc, oiDict, oiRef, dpObj, colorSpace)
			fixes = append(fixes, fmt.Sprintf("replaced invalid ICC profile in OutputIntent[%d] (version=%d, class=%q, cs=%q)", i, version, deviceClass, colorSpace))
		} else if version != 2 && compliance.PDFAPart(profile) == 1 {
			// PDF/A-1 rests on PDF 1.4, which predates ICC v4, so ISO
			// 19005-1, 6.2.2 takes the destination profile of PDF Reference
			// 1.4, 9.10.4, a version 2 one (veraPDF: "version < 3.0"). The
			// output intent names a printing or display condition, which a
			// stock profile of the same colour space does not characterize,
			// so the profile is only ever re-encoded as version 2 with the
			// same colour transforms, and kept as it is when it cannot be.
			if convertOutputProfileToV2(ctx, alloc, doc, oiDict, oiRef, dpObj, stream, data, converted) {
				fixes = append(fixes, fmt.Sprintf("re-encoded the version %d ICC profile of OutputIntent[%d] as version 2", version, i))
			}
		}

		newOIArr[i] = oiEntry
	}

	return fixes, nil
}

// convertOutputProfileToV2 replaces an output intent's version 4 destination
// profile with its version 2 re-encoding ([icc.ToV2]), which converts colours
// as the source does. It reports whether it wrote one: a profile whose tags
// version 2 cannot hold without resampling stays, and so does its violation.
//
// An indirect profile stream is rewritten at its own object number, so every
// output intent and ICCBased colour space sharing it reads the version 2
// profile, which ISO 19005-1, 6.2.3.2 asks of an ICCBased profile as well, and
// the intents keep naming one indirect object (6.2.2 t2). converted records
// the streams already rewritten, so an intent sharing one reports no second
// fix.
func convertOutputProfileToV2(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, oiDict ast.PdfDict, oiRef ast.PdfIndirectRef, dpObj ast.PdfObject, stream ast.PdfStream, data []byte, converted map[int]bool) bool {
	dpRef, isRef := dpObj.(ast.PdfIndirectRef)
	if isRef && converted[dpRef.ObjectNum] {
		return false
	}
	v2, err := icc.ToV2(data)
	if err != nil {
		doc.Logger.Debug("remediation: corrupt-icc: version 4 output profile has no lossless version 2 form",
			slog.Any("err", err))
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	// The profile is encoded afresh, so the filter chain describing the old
	// body no longer applies. A sampled curve is larger than the parametric
	// one it replaces, and Flate, which every PDF/A part admits, keeps it
	// near the source's size.
	newDict := make(ast.PdfDict, len(stream.Dict))
	maps.Copy(newDict, stream.Dict)
	delete(newDict, "Filter")
	delete(newDict, "DecodeParms")
	delete(newDict, "DL")
	newStream := flateStream(newDict, v2)
	if isRef && dpRef.ObjectNum > 0 {
		alloc.AllocAt(dpRef.ObjectNum, newStream)
		converted[dpRef.ObjectNum] = true
		return true
	}
	if oiRef.ObjectNum == 0 {
		return false
	}
	newOI := make(ast.PdfDict, len(oiDict))
	maps.Copy(newOI, oiDict)
	newOI["DestOutputProfile"] = alloc.Alloc(newStream)
	alloc.AllocAt(oiRef.ObjectNum, newOI)
	return true
}

// replaceICCProfile replaces the DestOutputProfile stream with a stock profile
// matching colorSpace: the embedded CMYK profile when it is "CMYK", the
// generated version 2 grey profile when it is "GRAY", sRGB otherwise. An empty
// colorSpace means the original profile could not be read, so sRGB is the only
// defensible choice. Every stock profile is version 2, which every PDF/A part
// admits (ISO 19005-1, 6.2.2; ISO 19005-2, 6.2.3).
//
// Keeping the colour space matters because /OutputConditionIdentifier is left
// untouched here. Substituting an RGB profile under a CMYK output condition
// leaves the intent describing a destination it no longer converts to, and
// under a grey one it also turns the intent into an RGB intent, which licenses
// DeviceRGB content the source never did (ISO 19005-1, 6.2.3.3).
func replaceICCProfile(alloc *writer.ObjAllocator, oiDict ast.PdfDict, oiRef ast.PdfIndirectRef, dpObj ast.PdfObject, colorSpace string) {
	iccData := sRGBICCProfile
	nComponents := 3
	switch colorSpace {
	case "CMYK":
		iccData = cmykICCProfile
		nComponents = 4
	case "GRAY":
		iccData = sGrayICCProfile
		nComponents = 1
	}

	// Create replacement stream.
	newStream := ast.NewStream(ast.PdfDict{
		"N": ast.PdfInt(nComponents),
	}, iccData)
	newStreamRef := alloc.Alloc(newStream)

	// Update the OutputIntent dict.
	newOI := make(ast.PdfDict, len(oiDict))
	maps.Copy(newOI, oiDict)
	newOI["DestOutputProfile"] = newStreamRef

	if oiRef.ObjectNum > 0 {
		alloc.AllocAt(oiRef.ObjectNum, newOI)
	} else if dpRef, ok := dpObj.(ast.PdfIndirectRef); ok {
		// Replace the stream object directly.
		alloc.AllocAt(dpRef.ObjectNum, newStream)
	}
}
